package approval

// 変更の記録(4.6)と承認の記録、正規化とハッシュ。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/r2fish/sitate-backup/internal/contract"
)

const timeLayout = "2006-01-02T15:04:05Z"

var (
	idRe      = regexp.MustCompile(`^c-[0-9]{8}-[0-9]{4,}$`)
	timeRe    = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)
	sigB64Re  = regexp.MustCompile(`^[A-Za-z0-9+/]{86}==$`)
	hashShape = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// 署名の対象の接頭辞(4.6)。
const signPrefix = "sitate-approval/v1\n"

type changeBody struct {
	ChangeID         string          `json:"change_id"`
	Op               string          `json:"op"`
	Args             json.RawMessage `json:"args"`
	Target           json.RawMessage `json:"target"`
	PreconditionHash string          `json:"precondition_hash"`
	Plan             json.RawMessage `json:"plan"`
	ProposedAt       string          `json:"proposed_at"`
	DelayHours       int             `json:"delay_hours"`
	ExpiresAt        string          `json:"expires_at"`
}

type changeRec struct {
	changeBody
	CanonicalHash string `json:"canonical_hash"`
}

type approverRec struct {
	KeyFP      string `json:"key_fp"`
	KeyType    string `json:"key_type"`
	SignedAt   string `json:"signed_at"`
	Sig        string `json:"sig"`
	ReceivedAt string `json:"received_at"`
}

type approvalRec struct {
	ChangeID      string        `json:"change_id"`
	CanonicalHash string        `json:"canonical_hash"`
	Approvers     []approverRec `json:"approvers"`
	NotifyProofOK bool          `json:"notify_proof_ok,omitempty"`
}

func fmtTime(t time.Time) string { return t.UTC().Truncate(time.Second).Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	if !timeRe.MatchString(s) {
		return time.Time{}, fmt.Errorf("time format")
	}
	return time.Parse(timeLayout, s)
}

// hashOfRaw は記録の本体(canonical_hash 自身を除く)の RFC 8785 直列化の SHA-256。保存した JSON の字面から計算し直す(書き換えられた項目を取りこぼさない)。
func hashOfRaw(raw []byte) (string, error) {
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", err
	}
	delete(m, "canonical_hash")
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return contract.CanonicalHash(b)
}

// signedText は署名の対象 "sitate-approval/v1\n" + canonical_hash + "\n" + signed_at(4.6)。
func signedText(canonicalHash, signedAt string) []byte {
	return []byte(signPrefix + canonicalHash + "\n" + signedAt)
}

func changeName(id string) string   { return changesDir + "/" + id + ".json" }
func approvalName(id string) string { return approvDir + "/" + id + ".json" }

// nextChangeID は pending/changes/ の同じ UTC の日の最大の連番に 1 を足す(4.6。切り捨てた記録のファイルも数える)。
func (s *pstore) nextChangeID(day string) (string, error) {
	names, err := s.b.list(changesDir)
	if err != nil {
		return "", err
	}
	max := 0
	prefix := "c-" + day + "-"
	for _, n := range names {
		id := strings.TrimSuffix(n, ".json")
		if strings.HasPrefix(id, prefix) {
			if v, err := strconv.Atoi(strings.TrimPrefix(id, prefix)); err == nil && v > max {
				max = v
			}
		}
	}
	return fmt.Sprintf("c-%s-%04d", day, max+1), nil
}

func (s *pstore) readChange(id string) (changeRec, []byte, error) {
	var rec changeRec
	if !idRe.MatchString(id) {
		return rec, nil, stateErr(id, nil)
	}
	raw, ok, err := s.b.read(changeName(id))
	if err != nil {
		return rec, nil, err
	}
	if !ok {
		return rec, nil, stateErr(id, nil)
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return rec, raw, sigErr("hash_mismatch", nil)
	}
	return rec, raw, nil
}

func (s *pstore) readApproval(id string) (approvalRec, bool, error) {
	var ap approvalRec
	raw, ok, err := s.b.read(approvalName(id))
	if err != nil || !ok {
		return ap, false, err
	}
	if err := json.Unmarshal(raw, &ap); err != nil {
		return ap, false, sigErr("bad_signature", nil)
	}
	return ap, true, nil
}

func marshalCanonical(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return contract.CanonicalJSON(b)
}

// delayRec は state.json の delays[] の要素(4.5)。
type delayRec struct {
	ChangeID           string          `json:"change_id"`
	DelayExtendedHours int             `json:"delay_extended_hours"`
	ApplyingSoonSentAt *string         `json:"applying_soon_sent_at"`
	FirstAcceptedAt    *string         `json:"first_accepted_at"`
	Deliveries         []deliveryGroup `json:"deliveries"`
}

// deliveryGroup は 1 回の通知イベントの配送の結果(type はイベントの種類、delivery は宛先ごと。event.json の delivery と同じ)。
type deliveryGroup struct {
	Type     string        `json:"type"`
	Delivery []deliveryRec `json:"delivery"`
}

type deliveryRec struct {
	To         string  `json:"to"`
	Channel    string  `json:"channel"`
	Delivered  bool    `json:"delivered"`
	Attempts   int     `json:"attempts"`
	Reason     *string `json:"reason"`
	AcceptedAt *string `json:"accepted_at"`
}

// notBefore は遅延の基点(4.6): max(received_at + delay_hours + delay_extended_hours, first_accepted_at + delay_hours)。signed_at は使わない。
func notBefore(ap approvalRec, delayHours int, d *delayRec) (time.Time, error) {
	if len(ap.Approvers) == 0 {
		return time.Time{}, fmt.Errorf("no approver")
	}
	rcv, err := parseTime(ap.Approvers[0].ReceivedAt)
	if err != nil {
		return time.Time{}, err
	}
	delay := time.Duration(delayHours) * time.Hour
	nb := rcv.Add(delay)
	if d != nil {
		nb = nb.Add(time.Duration(d.DelayExtendedHours) * time.Hour)
		if d.FirstAcceptedAt != nil {
			if fa, err := parseTime(*d.FirstAcceptedAt); err == nil && fa.Add(delay).After(nb) {
				nb = fa.Add(delay)
			}
		}
	}
	return nb, nil
}

type (
	jsonRaw = json.RawMessage
	ctxT    = context.Context
)

func unmarshalJSON(b []byte, v any) error { return json.Unmarshal(b, v) }

func asContractErr(err error, target **contract.Error) bool { return errors.As(err, target) }
