import { expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

test("browser writes without a storage driver reject instead of reporting success", () => {
    const result = spawnSync(process.execPath, [fileURLToPath(new URL("./helpers/localforage-storage.worker.ts", import.meta.url))], { encoding: "utf8" });
    expect(result.stderr).toBe("");
    expect(result.status).toBe(0);
});
