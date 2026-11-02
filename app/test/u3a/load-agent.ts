// エージェントのプロセスが通る経路を一通り実行する: 実行基盤の読み込み、クライアントの作成、通常の要求、Tool Runner の往復、モデルの存在確認。
import { createAgentRuntime } from "../../src/agent/runtime.ts";
import { selectModel } from "../../src/agent/llm-errors.ts";
import { fakeLlm } from "./fake-llm.ts";

const llm = fakeLlm([{ toolUses: [{ name: "t", input: {} }] }, { text: "ok" }]);
const rt = createAgentRuntime({ client: llm.client, model: "m", system: [], tools: [{ name: "t", description: "d", input_schema: { type: "object", properties: {} } }], onToolCall: async () => "x" });
await rt.run("x");
await selectModel(llm.client, ["m"]).catch(() => null);
console.log("DONE");
