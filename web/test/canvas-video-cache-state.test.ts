import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

test("a succeeded task leaves loading state when durable video caching fails", () => {
    const source = readFileSync(resolve(import.meta.dir, "../src/pages/canvas/use-canvas-generation.ts"), "utf8");
    const fallback = source.slice(source.indexOf('if (task.status === "succeeded")'), source.indexOf("} else {", source.indexOf('if (task.status === "succeeded")')));

    expect(fallback).toContain("status: NODE_STATUS_ERROR");
    expect(fallback).toContain("resourceReloadAvailable: generationTaskCanReloadResource(task)");
});
