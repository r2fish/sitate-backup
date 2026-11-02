package recv

import "embed"

// Files は受け口の sh を核へ埋め込んだもの(go:embed)。acquire はこれを import してよい(spec 7.5 の依存の規則の例外)。
// sh の中身は U2a が書く。骨格の sitate-recv は NOT_IMPLEMENTED を返す。
//
//go:embed sitate-recv source-setup
var Files embed.FS
