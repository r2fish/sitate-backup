// ドライバの操作の窓口(Driver がこれを満たす。試験では偽の画面を差し込む)。
import type { PendingView } from "./driver.ts";
import type { SurfaceName } from "./driver.ts";
import type { TestEvent } from "../../src/testing/driver-server.ts";

export interface Port {
  lines(): Promise<{ role: string; text: string }[]>;
  pending(): Promise<PendingView>;
  events(): Promise<TestEvent[]>;
  chat(text: string): Promise<void>;
  fill(surface: SurfaceName, target: string, value: string): Promise<boolean>;
  click(surface: SurfaceName, action: string): Promise<boolean>;
}
