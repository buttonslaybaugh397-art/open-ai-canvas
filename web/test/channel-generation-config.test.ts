import { afterEach, describe, expect, test } from "bun:test";

import { backendProviderConfig, prepareBackendGenerationTask, prepareBackendToolGenerationTask } from "../src/services/api/generation-task";
import { createModelChannel, defaultConfig, type AiConfig } from "../src/stores/use-config-store";
import { usePluginStore } from "../src/stores/use-plugin-store";

const originalRuntimeStatuses = usePluginStore.getState().runtimeStatuses;

afterEach(() => {
    usePluginStore.setState({ runtimeStatuses: originalRuntimeStatuses });
});

function channelConfig(): AiConfig {
    return {
        ...defaultConfig,
        model: "selected::shared-model",
        channels: [
            createModelChannel({
                id: "other",
                models: ["shared-model"],
                interfaceType: "chat-completion",
                headers: [{ name: "X-Test-Route", value: "other" }],
            }),
            createModelChannel({
                id: "selected",
                models: ["shared-model"],
                interfaceType: "chat-completion",
                baseUrl: "https://synthetic.example/v1",
                apiKey: "synthetic-test-key",
                headers: [
                    { name: "X-Test-Route", value: "selected" },
                    { name: "User-Agent", value: "SyntheticAgent/1.0" },
                ],
            }),
        ],
    };
}

describe("channel generation configuration", () => {
    test("prepares a TianYue task with its protocol and real duration unchanged", async () => {
        const config: AiConfig = {
            ...defaultConfig,
            model: "tianyue::configured-model",
            videoSeconds: "8",
            vquality: "720p",
            size: "16:9",
            channels: [
                createModelChannel({
                    id: "tianyue",
                    interfaceType: "tianyue-video",
                    baseUrl: "https://api.tianyue.xyz",
                    apiKey: "synthetic-key",
                    models: ["configured-model"],
                    modelCosts: [{ model: "configured-model", capability: "video", billingMode: "fixed_request", unitPriceMicrocredits: 1 }],
                }),
            ],
        };
        const task = await prepareBackendGenerationTask({ mode: "video", prompt: "test", config });
        expect(task.input.config).toMatchObject({
            interfaceType: "tianyue-video",
            baseUrl: "https://api.tianyue.xyz",
            model: "configured-model",
            videoSeconds: "8",
            vquality: "720p",
        });
        expect(task.input.config).not.toHaveProperty("duration");
    });

    for (const mode of ["text", "image", "video", "audio"] as const) {
        test(`preserves the selected channel headers in a prepared ${mode} task`, async () => {
            const config = channelConfig();
            const task = await prepareBackendGenerationTask({ mode, prompt: "synthetic prompt", config });

            expect(task.input.config.headers).toEqual(config.channels[1]!.headers);
            expect(task.input.config.headers).not.toEqual(config.channels[0]!.headers);
        });
    }

    test("preserves custom headers in a prepared tool generation task", () => {
        const config = channelConfig();
        const task = prepareBackendToolGenerationTask({
            config,
            prompt: "synthetic prompt",
            messages: [{ role: "user", content: "synthetic prompt" }],
            tools: [],
            toolChoice: "auto",
        });

        expect(task.input.config.headers).toEqual(config.channels[1]!.headers);
    });

    test("keeps an empty header list for an unconfigured channel", () => {
        const config = channelConfig();
        config.channels[1] = createModelChannel({ ...config.channels[1], headers: [] });

        expect(backendProviderConfig(config)).toHaveProperty("headers", []);
    });

    test("does not expose channel headers in a managed logical model task", () => {
        const config = channelConfig();
        config.channels[1] = createModelChannel({
            ...config.channels[1],
            scope: "system",
            modelCosts: [{ model: "shared-model", capability: "text", logicalModelId: "logical-test", billingMode: "fixed_request", unitPriceMicrocredits: 0 }],
        });

        expect(backendProviderConfig(config, "text")).not.toHaveProperty("headers");
    });

    for (const provider of ["runninghub", "comfyui"] as const) {
        test(`does not inherit ordinary channel headers in a ${provider} task`, () => {
            usePluginStore.setState({ runtimeStatuses: { "runninghub-workflow-provider": "enabled", "comfyui-workflow-provider": "enabled" } });
            const config = channelConfig();
            config.taskWorkflowProvider = provider;
            config.runningHub = {
                ...config.runningHub,
                enabled: true,
                apiKey: "synthetic-workflow-key",
                workflowId: "workflow-test",
                workflows: [{ workflowId: "workflow-test", capability: "image", fields: [] }],
            };
            config.comfyBridge = {
                ...config.comfyBridge,
                enabled: true,
                bridgeId: "bridge-test",
                workflowId: "workflow-test",
                workflows: [{ workflowId: "workflow-test", capability: "image", fields: [] }],
            };

            expect(backendProviderConfig(config, "image")).toHaveProperty("headers", []);
        });
    }
});
