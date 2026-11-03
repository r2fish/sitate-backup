// 人の役(台本の固定の回答)で 5 面のうち人の操作の面(秘密の入力ボックス、承認カード、フレーズのパネル)を操作する。裁量を入れない。
// 台本に無い面の要求は終了コード 2。入れた値は記録に出さない(種類と面の名前だけ)。
import type { Port } from "./port.ts";
import type { PendingView } from "./driver.ts";
import type { Recorder } from "./record.ts";
import type { Answers } from "./script.ts";

export interface Outcome { readonly exit: 0 | 1 | 2 | 3; readonly reason: string }
const L = "chat.secret.label.";
const PHRASE_VERIFY = "phrase.verify.input_label";
const MAX_WAIT_CYCLES = 100;

export interface HumanDeps {
  readonly port: Port;
  readonly answers: Answers;
  readonly rec: Recorder;
  readonly catalog: Readonly<Record<string, string>>;
  readonly hookWord: (recipient: string) => string | null;
}

export class Human {
  readonly #d: HumanDeps;
  readonly #keyOfLabel = new Map<string, string>();
  readonly #done = new Set<string>();
  #llmBoxes = 0;
  #words: string[] = [];
  #handwritten = false;
  #confirmed = false;
  #verifies = 0;
  #waits = 0;
  readonly humanWork = new Set<string>();

  constructor(d: HumanDeps) {
    this.#d = d;
    for (const [k, v] of Object.entries(d.catalog)) if (k.startsWith(L) || k === PHRASE_VERIFY) this.#keyOfLabel.set(v, k);
  }

  /** 人の操作の面が開いていれば 1 つ処理する。何もなければ null。処理したら true、終わりにするなら Outcome。 */
  async handle(p: PendingView): Promise<boolean | Outcome> {
    if (p.phrase !== null && p.phrase.words.length > 0) this.#words = p.phrase.words;
    if (p.secret !== null) return this.#secret(p.secret);
    if (p.card !== null) return this.#card(p.card);
    if (p.phrase !== null) return this.#phrase(p.phrase);
    return false;
  }

  #work(id: string, detail: string): void {
    if (this.humanWork.has(id)) return;
    this.humanWork.add(id);
    this.#d.rec.write("human_work", { id, detail });
  }

  async #secret(label: string): Promise<boolean | Outcome> {
    const key = this.#keyOfLabel.get(label) ?? null;
    const a = this.#d.answers;
    let value: string;
    switch (key) {
      case L + "llm": this.#llmBoxes += 1; if (this.#llmBoxes > 1) return { exit: 1, reason: "LLM API キーの入力ボックスが再び開いた(キーが受け付けられなかった)" }; value = a.llmKey; break;
      case L + "admin": case L + "source_admin": value = a.adminKeyPem; break;
      case L + "dest_admin": this.#work("H3", "復元先は試験環境の復元先ホストを使う"); value = a.adminKeyPem; break;
      case L + "storage_admin": this.#work("H1", "保管先の管理用鍵を秘密の入力ボックスへ入れる"); value = a.storageAdmin; break;
      case L + "webhook": value = a.hookUrl; break;
      case PHRASE_VERIFY: value = this.#words.join(" "); break;
      default: return { exit: 2, reason: `台本に無い秘密の入力ボックス(${key ?? "文が未知"})` };
    }
    const filled = await this.#d.port.fill("secret-box", "none", value);
    const clicked = filled && (await this.#d.port.click("secret-box", "secret-submit"));
    this.#d.rec.write("surface_op", { surface: "secret-box", label_key: key, filled, submitted: clicked });
    return clicked ? true : { exit: 1, reason: `秘密の入力ボックスに入れられない(${key})` };
  }

  async #card(card: NonNullable<PendingView["card"]>): Promise<boolean | Outcome> {
    const title = this.#d.catalog["card.op.seal.title"] ?? "";
    if (title === "" || !card.text.includes(title)) return { exit: 2, reason: "台本に無い承認カード(seal 以外)", };
    const words = card.proofs.map((r) => [r, this.#d.hookWord(r)] as const);
    const approve = card.actions.find((x) => x.action === "approve");
    if (words.some(([, w]) => w === null) || approve === undefined || approve.disabled) {
      this.#waits += 1;
      return this.#waits > MAX_WAIT_CYCLES ? { exit: 1, reason: "承認カードの確認語が受け手に届かない、または承認が押せない" } : true;
    }
    for (const [r, w] of words) await this.#d.port.fill("approval-card", r, w!);
    this.#work("H4", "seal の承認カードで宛先ごとの確認語を入力");
    const ok = await this.#d.port.click("approval-card", "approve");
    this.#waits = 0;
    this.#d.rec.write("surface_op", { surface: "approval-card", action: "approve", proofs_entered: words.length, clicked: ok });
    return ok ? true : { exit: 1, reason: "承認を押せない" };
  }

  async #phrase(ph: NonNullable<PendingView["phrase"]>): Promise<boolean | Outcome> {
    const act = async (action: string): Promise<boolean> => { const ok = await this.#d.port.click("phrase-panel", action); this.#waits = 0; this.#d.rec.write("surface_op", { surface: "phrase-panel", action, clicked: ok }); return ok; };
    if (ph.actions.includes("handwritten") && !this.#handwritten) { this.#handwritten = true; return act("handwritten"); }
    if (this.#verifies === 0 && ph.actions.includes("verify")) { this.#verifies += 1; return act("verify"); }
    if (ph.actions.includes("confirm") && this.#verifies > 0 && !this.#confirmed) { this.#confirmed = true; this.#work("H2", "フレーズを控えたことにする"); return act("confirm"); }
    if (ph.notice !== null && this.#verifies > 0 && this.#verifies < 3 && ph.actions.includes("verify")) { this.#verifies += 1; return act("verify"); }
    this.#waits += 1;
    return this.#waits > MAX_WAIT_CYCLES ? { exit: 1, reason: "フレーズのパネルが進まない" } : true;
  }
}
