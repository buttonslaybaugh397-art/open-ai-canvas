import { describe, expect, test } from "bun:test";

import { testChannelModelConnection } from "../src/lib/model-connection-test";
import type { BackendGenerationResult, GenerationTaskDependencies } from "../src/services/api/generation-task";
import type { CreateTaskInput, GenerationTask } from "../src/services/api/task-center";
import { createModelChannel, type ModelCapability } from "../src/stores/use-config-store";

function testRuntime(result: BackendGenerationResult) {
    const submitted: CreateTaskInput[] = [];
    const initial: GenerationTask = {
        id: "test-task",
        type: "canvas_text",
        status: "queued",
        prompt: "",
        attempts: 0,
        createdAt: "",
        updatedAt: "",
    };
    const dependencies: GenerationTaskDependencies = {
        createTask: async (input) => {
            submitted.push(input);
            return { ...initial, type: input.type };
        },
        waitTask: async () => ({ ...initial, status: "succeeded", resultJson: JSON.stringify(result) }),
        runLocal: async () => {
            throw new Error("unexpected local runtime");
        },
        createId: () => "test-id",
        now: () => "",
    };
    return { submitted, dependencies };
}

const channel = createModelChannel({
    id: "test-channel",
    name: "Test Channel",
    baseUrl: "https://provider.example/openapi",
    apiKey: "test-key",
    models: ["test-model"],
});
describe("channel connection tests use the installed backend protocol", () => {
    const cases: Array<[string, ModelCapability, BackendGenerationResult]> = [
        ["deepseek-chat", "text", { mode: "text", text: "OK" }],
        ["gemini-generate-content", "text", { mode: "text", text: "OK" }],
        ["globalaiopc-image", "image", { mode: "image", images: [{ dataUrl: "", storageKey: "resource:test-image" }] }],
        ["aistarslab-image", "image", { mode: "image", images: [{ dataUrl: "https://result.example/image" }] }],
        ["globalaiopc-video", "video", { mode: "video", video: { dataUrl: "", storageKey: "resource:test-video" } }],
        ["huiquyun-video", "video", { mode: "video", video: { dataUrl: "https://result.example/video" } }],
        ["aistarslab-video", "video", { mode: "video", video: { dataUrl: "https://result.example/video" } }],
        ["weijin-video", "video", { mode: "video", video: { dataUrl: "https://result.example/video" } }],
        ["tianyue-video", "video", { mode: "video", video: { dataUrl: "https://result.example/video" } }],
        ["kling-video", "video", { mode: "video", video: { dataUrl: "https://result.example/video" } }],
        ["openai-audio", "audio", { mode: "audio", audio: { dataUrl: "https://result.example/audio" } }],
    ];
    for (const [protocol, capability, result] of cases) {
        test(`${protocol} preserves its protocol and waits for a valid result`, async () => {
            const { dependencies, submitted } = testRuntime(result);
            const detail = await testChannelModelConnection(channel, "test-model", capability, protocol, dependencies);
            expect(detail).toContain("正常");
            expect(submitted).toHaveLength(1);
            expect(submitted[0].type).toBe(`canvas_${capability}`);
            expect(submitted[0].input).toMatchObject({
                mode: capability,
                config: { interfaceType: protocol, model: "test-model", baseUrl: channel.baseUrl },
                metadata: { source: "channel-model-test" },
            });
        });
    }

    test("Gemini channel does not drop the selected protocol before its first save", async () => {
        const { dependencies, submitted } = testRuntime({ mode: "text", text: "OK" });
        await testChannelModelConnection({ ...channel, apiFormat: "gemini" }, "test-model", "text", "gemini-generate-content", dependencies);
        expect(submitted[0].input).toMatchObject({ config: { interfaceType: "gemini-generate-content" } });
    });

    test("a queued task is not reported as a successful model test", async () => {
        const { dependencies } = testRuntime({ mode: "video", video: { dataUrl: "https://result.example/video" } });
        dependencies.waitTask = async () => {
            throw new Error("provider rejected model");
        };
        await expect(testChannelModelConnection(channel, "test-model", "video", "kling-video", dependencies)).rejects.toThrow("provider rejected model");
    });

    test("an empty successful envelope is not a valid generation result", async () => {
        const { dependencies } = testRuntime({ mode: "video" });
        await expect(testChannelModelConnection(channel, "test-model", "video", "kling-video", dependencies)).rejects.toThrow("未返回有效结果");
    });

    test("missing protocol fails before submitting any task", async () => {
        const { dependencies, submitted } = testRuntime({ mode: "text", text: "OK" });
        await expect(testChannelModelConnection(channel, "test-model", "text", "", dependencies)).rejects.toThrow("请求协议插件");
        expect(submitted).toHaveLength(0);
    });
});
