// 実行時にどのモジュールが評価されるかを記録する(boundary-runtime.test.ts が子プロセスで使う)。
import { register } from "node:module";
register("data:text/javascript," + encodeURIComponent(`export async function load(url, ctx, next){ if(/@anthropic-ai\\/sdk\\/(internal\\/node|tools\\/)/.test(url)) console.log("LOADED " + url.replace(/.*node_modules\\//,"")); return next(url, ctx); }`));
