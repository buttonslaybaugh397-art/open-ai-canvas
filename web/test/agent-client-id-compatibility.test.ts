import { expect, test } from "bun:test";

test("Agent 提交和插话不依赖仅安全上下文可用的 randomUUID", async () => {
    const panel = await Bun.file(new URL("../src/components/canvas/canvas-cloud-agent-panel.tsx", import.meta.url)).text();

    expect(panel).not.toContain("crypto.randomUUID");
    expect(panel).toContain("const messageId = `user-${nanoid()}`;");
    expect(panel).toContain("const key = pending?.key || nanoid();");
});
