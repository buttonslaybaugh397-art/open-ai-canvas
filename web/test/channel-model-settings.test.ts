import { describe, expect, test } from "bun:test";
import { App } from "antd";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { defaultModelCapabilityConfig } from "../src/lib/model-capabilities";
import type { ModelProtocolDefinition } from "../src/lib/model-protocols";
import { resolveChannelModelSettings, updateChannelModelSettings } from "../src/pages/settings/channel-model-settings";
import { ChannelModelSettings } from "../src/pages/settings/channel-video-pricing";
import { createModelChannel, defaultConfig, resolveModelRequestConfig } from "../src/stores/use-config-store";

function definition(value: string, capability: ModelProtocolDefinition["capability"], enabled = true): ModelProtocolDefinition {
    return { value, capability, enabled, label: value, create: `POST /${value}`, contentType: "application/json", media: "test" };
}

const protocols = [definition("chat-completion", "text"), definition("openai-image", "image"), definition("gemini-image", "image"), definition("gemini-veo", "video"), definition("tianyue-video", "video"), definition("openai-audio", "audio")];

function configuredChannel(protocol?: string) {
    return createModelChannel({
        id: "selected",
        models: ["test-model", "other-model"],
        modelCosts: [
            { model: "other-model", capability: "text", protocol: "chat-completion", billingMode: "fixed_request", unitPriceMicrocredits: 2 },
            {
                model: "test-model",
                capability: "image",
                protocol,
                displayName: "Configured model",
                description: "Keep this metadata",
                icon: "test-icon",
                billingMode: "per_second",
                unitPriceMicrocredits: 1234,
                defaultOptions: { quality: "custom" },
                capabilityConfig: defaultModelCapabilityConfig("gemini-image", "test-model"),
            },
        ],
    });
}

describe("channel model settings use confirmed protocols", () => {
    test("uses the installed TianYue video protocol for manually configured opaque models", () => {
        const channel = createModelChannel({ interfaceType: "tianyue-video", models: ["opaque-model"] });
        expect(resolveChannelModelSettings(channel, "opaque-model", protocols)).toMatchObject({
            protocol: "tianyue-video",
            capability: "video",
            availableProtocol: { value: "tianyue-video" },
        });
        const disabled = protocols.map((item) => ({ ...item, enabled: item.value !== "tianyue-video" }));
        expect(resolveChannelModelSettings(channel, "opaque-model", disabled).availableProtocol).toBeUndefined();
    });

    test("does not infer a protocol from the model name, connection format, or available catalog", () => {
        const channel = createModelChannel({ apiFormat: "gemini", models: ["gemini-image-model"] });
        const before = structuredClone(channel);
        const settings = resolveChannelModelSettings(channel, "gemini-image-model", protocols);

        expect(settings.capability).toBe("image");
        expect(settings.protocol).toBeUndefined();
        expect(settings.availableProtocol).toBeUndefined();
        expect(channel).toEqual(before);
    });

    test("a catalog model with an undefined protocol remains unconfigured", () => {
        const channel = configuredChannel();
        const settings = resolveChannelModelSettings(channel, "test-model", protocols);

        expect(settings.protocol).toBeUndefined();
        expect(settings.availableProtocol).toBeUndefined();
        expect(settings.cost).toBe(channel.modelCosts![1]);
    });

    test("inherits an enabled channel protocol without persisting a per-model override", () => {
        const channel = { ...configuredChannel(), interfaceType: "gemini-image" };
        const settings = resolveChannelModelSettings(channel, "test-model", protocols);

        expect(settings.protocol).toBe("gemini-image");
        expect(settings.availableProtocol?.value).toBe("gemini-image");
        expect(channel.modelCosts![1]!.protocol).toBeUndefined();
    });

    test("infers the capability from the configured channel protocol for an opaque new model", () => {
        const channel = createModelChannel({ interfaceType: "gemini-veo", models: ["opaque-model"] });

        expect(resolveChannelModelSettings(channel, "opaque-model", protocols)).toMatchObject({
            protocol: "gemini-veo",
            capability: "video",
            availableProtocol: { value: "gemini-veo" },
        });
    });

    test("prefers the saved model protocol to the channel protocol", () => {
        const channel = { ...configuredChannel("openai-image"), interfaceType: "gemini-image" };
        const settings = resolveChannelModelSettings(channel, "test-model", protocols);

        expect(settings.protocol).toBe("openai-image");
        expect(settings.availableProtocol?.value).toBe("openai-image");
    });

    for (const catalog of [[], protocols.map((item) => ({ ...item, enabled: item.value !== "openai-image" }))]) {
        test(`does not replace an unavailable saved protocol with a valid inherited protocol (${catalog.length})`, () => {
            const channel = { ...configuredChannel("openai-image"), interfaceType: "gemini-image" };
            const settings = resolveChannelModelSettings(channel, "test-model", catalog);

            expect(settings.protocol).toBe("openai-image");
            expect(settings.availableProtocol).toBeUndefined();
            expect(channel.modelCosts![1]!.protocol).toBe("openai-image");
        });
    }

    test("an incompatible inherited protocol cannot be tested", () => {
        const channel = { ...configuredChannel(), interfaceType: "gemini-veo" };
        const settings = resolveChannelModelSettings(channel, "test-model", protocols);

        expect(settings.protocol).toBe("gemini-veo");
        expect(settings.capability).toBe("image");
        expect(settings.availableProtocol).toBeUndefined();
    });

    test("selecting a protocol persists exactly that value for normal generation", () => {
        const channel = configuredChannel();
        const before = structuredClone(channel);
        const modelCosts = updateChannelModelSettings(channel, "test-model", { protocol: "gemini-image" }, protocols);
        const updated = { ...channel, modelCosts };

        expect(modelCosts[1]).toEqual({ ...channel.modelCosts![1], protocol: "gemini-image" });
        expect(modelCosts[0]).toBe(channel.modelCosts![0]);
        expect(channel).toEqual(before);
        expect(resolveModelRequestConfig({ ...defaultConfig, channels: [updated] }, "selected::test-model").interfaceType).toBe("gemini-image");
        expect(resolveChannelModelSettings(updated, "test-model", protocols).availableProtocol?.value).toBe("gemini-image");
    });

    test("selecting a different protocol preserves pricing, metadata and capability settings", () => {
        const channel = configuredChannel("gemini-image");
        const modelCosts = updateChannelModelSettings(channel, "test-model", { protocol: "openai-image" }, protocols);

        expect(modelCosts[1]).toEqual({ ...channel.modelCosts![1], protocol: "openai-image" });
        expect(modelCosts[1]!.capabilityConfig).toBe(channel.modelCosts![1]!.capabilityConfig);
    });

    test("capability editing without a protocol does not insert a default", () => {
        const channel = configuredChannel();
        const modelCosts = updateChannelModelSettings(channel, "test-model", { capability: "video" }, protocols);

        expect(modelCosts[1]).toEqual({ ...channel.modelCosts![1], capability: "video" });
        expect(modelCosts[1]!.protocol).toBeUndefined();
    });

    test("capability editing preserves a saved protocol and requires an explicit compatible selection", () => {
        const channel = configuredChannel("gemini-image");
        const modelCosts = updateChannelModelSettings(channel, "test-model", { capability: "video" }, protocols);
        const settings = resolveChannelModelSettings({ ...channel, modelCosts }, "test-model", protocols);

        expect(modelCosts[1]).toEqual({ ...channel.modelCosts![1], capability: "video" });
        expect(settings.protocol).toBe("gemini-image");
        expect(settings.availableProtocol).toBeUndefined();
    });

    test("parameter editing without a protocol preserves metadata and other capability profiles", () => {
        const channel = configuredChannel();
        const original = channel.modelCosts![1]!.capabilityConfig!;
        const image = { ...original.image!, maxOutputs: 2 };
        const modelCosts = updateChannelModelSettings(channel, "test-model", { capabilityConfig: { version: original.version, image } }, protocols);

        expect(modelCosts[1]).toEqual({ ...channel.modelCosts![1], capabilityConfig: { ...original, image } });
        expect(modelCosts[1]!.protocol).toBeUndefined();
        expect(modelCosts[1]!.capabilityConfig!.video).toBe(original.video);
    });

    test("parameter editing with inheritance does not freeze a channel protocol into model settings", () => {
        const channel = { ...configuredChannel(), interfaceType: "gemini-image" };
        const modelCosts = updateChannelModelSettings(channel, "test-model", { capabilityConfig: { version: 1, image: defaultModelCapabilityConfig().image } }, protocols);

        expect(modelCosts[1]!.protocol).toBeUndefined();
        expect(resolveChannelModelSettings({ ...channel, interfaceType: "openai-image", modelCosts }, "test-model", protocols).availableProtocol?.value).toBe("openai-image");
    });

    test("a new model can be edited before choosing a protocol without inventing one", () => {
        const channel = createModelChannel({ models: ["opaque-model"] });
        const modelCosts = updateChannelModelSettings(channel, "opaque-model", { capability: "video" }, protocols);

        expect(modelCosts).toEqual([{ model: "opaque-model", capability: "video", billingMode: "fixed_request", unitPriceMicrocredits: 0 }]);
        expect(resolveChannelModelSettings({ ...channel, modelCosts }, "opaque-model", protocols).availableProtocol).toBeUndefined();
    });

    test("switching capability projects missing parameter controls without overwriting saved profiles", () => {
        const channel = configuredChannel();
        channel.modelCosts![1]!.capabilityConfig = { version: 1, image: defaultModelCapabilityConfig("gemini-image").image };
        const modelCosts = updateChannelModelSettings(channel, "test-model", { capability: "video" }, protocols);
        const settings = resolveChannelModelSettings({ ...channel, modelCosts }, "test-model", protocols);

        expect(settings.capabilityConfig?.video?.duration).toBeDefined();
        expect(settings.capabilityConfig?.image).toBe(channel.modelCosts![1]!.capabilityConfig.image);
        expect(modelCosts[1]!.capabilityConfig?.video).toBeUndefined();
        expect(modelCosts[1]!.protocol).toBeUndefined();
    });

    test("rejects missing, disabled or incompatible protocol selections without changing settings", () => {
        const channel = configuredChannel("gemini-image");
        const before = structuredClone(channel);
        for (const [protocol, catalog] of [
            ["unknown-plugin", protocols],
            ["gemini-veo", protocols],
            ["openai-image", protocols.map((item) => ({ ...item, enabled: item.value !== "openai-image" }))],
        ] as const) {
            expect(() => updateChannelModelSettings(channel, "test-model", { protocol }, catalog)).toThrow("已启用的请求协议");
        }
        expect(channel).toEqual(before);
    });

    test("does not recreate a model removed while its editor was open", () => {
        const channel = createModelChannel({ models: [] });

        expect(() => updateChannelModelSettings(channel, "missing-model", { protocol: "chat-completion" }, protocols)).toThrow("已不在渠道中");
        expect(channel.modelCosts).toBeUndefined();
    });

    test("renders an unconfigured model as pending configuration without writing anything", () => {
        const channel = createModelChannel({ models: ["gemini-image-model"] });
        let writes = 0;
        const html = renderToStaticMarkup(
            React.createElement(
                App,
                null,
                React.createElement(ChannelModelSettings, {
                    channel,
                    onChange: () => {
                        writes += 1;
                    },
                }),
            ),
        );

        expect(html).toContain("待配置请求协议");
        expect(writes).toBe(0);
        expect(channel.modelCosts).toBeUndefined();
    });
});
