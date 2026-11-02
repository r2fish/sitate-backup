import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
export const appRoot = join(here, "..", "..");
export const repoRoot = join(appRoot, "..");
export const contractSchemaDir = join(repoRoot, "internal", "contract", "schema");

export function readJson(path: string): any {
  return JSON.parse(readFileSync(path, "utf8"));
}

export function listFiles(dir: string, suffix: string): string[] {
  try {
    return readdirSync(dir).filter((f) => f.endsWith(suffix)).sort();
  } catch {
    return [];
  }
}
