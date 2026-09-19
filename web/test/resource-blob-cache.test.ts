import { expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

// Isolate the module's shared queues and stores from other tests' browser mocks.
test("resource cache retries, deduplicates, and isolates users and admin access", () => {
    const result = spawnSync(process.execPath, [fileURLToPath(new URL("./helpers/resource-blob-cache.worker.ts", import.meta.url))], { encoding: "utf8" });
    expect(result.stderr).toBe("");
    expect(result.status).toBe(0);
    expect(JSON.parse(result.stdout)).toEqual({ checks: 14 });
});
