// フレーズのパネル(U3a)と復旧キットの描画部(U3b)の間の型(作業指示書 5.1。D-0088 の 6)。
// `kit --format json` の fields + 24 語 + 言語 → 印刷物。置き場は U0-core が固定し、U0-core の持ち物。U3a・U3b は変えない。

/** 表示言語。イテレーション 1 は ja だけ描く(en はイテレーション 3)。 */
export type Lang = "ja" | "en";

/** 手順の id(`kit` の fields.steps[].id)。internal/contract/schema/data/kit-fields.json の enum と同じ。 */
export type KitStepId = "1" | "2" | "2-no-app" | "3" | "3-no-adapter" | "4-journal" | "4" | "5" | "6";

/** `kit --format json` の result.fields。internal/contract/schema/data/kit-fields.json の `fields` と欄の名前・形を揃える(app/test/contract/kit-types.test.ts が照らす)。 */
export interface KitFields {
  readonly kit_version: string;
  readonly issued_on: string;
  readonly core_version: string;
  readonly format_version: number;
  readonly distribution_url: string;
  readonly core_sha256: { readonly linux_x86_64: string; readonly linux_arm64: string };
  readonly sbom_sha256: string;
  readonly source_sha256: string;
  readonly signing_keys: readonly { readonly public_key: string; readonly fp: string }[];
  readonly storage: {
    readonly provider: string;
    readonly country: string | null;
    readonly endpoint: string;
    readonly bucket: string;
    readonly prefix: string;
    readonly repo_id: string;
    readonly keyring_id: string;
    readonly key_ref: string;
  };
  readonly host_key_fp: string;
  readonly bin_path: string;
  readonly steps: readonly { readonly id: KitStepId; readonly argv: readonly string[] }[];
  readonly provider_hints: { readonly kind: string; readonly key_name: string; readonly bin_prefix: string };
}

/** 24 語のフレーズ。画面に出したらすぐ消す。ログと結果に出さない。 */
export type PhraseWords = readonly string[] & { readonly length: 24 };

export interface KitRenderInput {
  readonly fields: KitFields;
  readonly phrase: PhraseWords;
  readonly lang: Lang;
}

/** 紙の面。表に「裏面」以外の欄を、裏に「裏面」の欄を刷る。 */
export type KitPage = "front" | "back";

/** 配置の欄の種類(spec 4.7 の表の欄に 1 つずつ対応する。説明文のブロックは explanation)。 */
export type KitBlockKind =
  | "title"
  | "product"
  | "installer"
  | "signing_keys"
  | "storage"
  | "restore_step"
  | "restore_proof"
  | "phrase_entry"
  | "host_key"
  | "custody"
  | "provider_notes"
  | "production_restore_notes"
  | "explanation";

/** 欄の位置と大きさ。A4 縦の用紙の左上を原点にしたミリメートル(画素ではない)。 */
export interface KitBox {
  readonly x_mm: number;
  readonly y_mm: number;
  readonly w_mm: number;
  readonly h_mm: number;
}

/** 配置の 1 欄。restore_step のときだけ step_id を持つ。 */
export interface KitBlock {
  readonly kind: KitBlockKind;
  readonly page: KitPage;
  /** 面の中の読む順(0 から。同じ面で重複しない)。 */
  readonly order: number;
  readonly box: KitBox;
  readonly step_id?: KitStepId;
}

/** 配置の構造。試験は画素でなくこれを比べる(欄の種類と順序と位置)。 */
export interface KitLayout {
  readonly paper: "A4-portrait";
  readonly blocks: readonly KitBlock[];
}

/** 印刷物(HTML 1 文書。印刷用の日本語フォントを含む)と、その配置の構造。 */
export interface KitPrintable {
  readonly html: string;
  readonly layout: KitLayout;
}

/** 収まらない・描けないときの失敗。印刷しない。reason に 24 語を含めない。 */
export interface KitRenderFailure {
  readonly ok: false;
  readonly reason: "does_not_fit" | "lang_unsupported";
  /** 収まらなかった欄(does_not_fit のとき)。 */
  readonly field?: keyof KitFields;
}

export interface KitRenderSuccess {
  readonly ok: true;
  readonly printable: KitPrintable;
}

export type KitRenderResult = KitRenderSuccess | KitRenderFailure;

/** 描画部の入口。U3b が実装する。fields が長くて 1 枚に収まらないときは ok: false を返し、HTML を作らない。 */
export type RenderKit = (input: KitRenderInput) => KitRenderResult;
