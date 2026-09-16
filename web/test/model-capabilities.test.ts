import assert from "node:assert/strict";
import test from "node:test";

// Bun 直接执行 TypeScript 测试时需要保留扩展名；生产 tsconfig 不包含 test/。
import { defaultModelCapabilityConfig, modelCapabilityConfigFor, normalizeVideoValue } from "../src/lib/model-capabilities.ts";

test("switching to MiniMax H3 replaces an unsupported 720p value with 768P", () => {
    const profile = defaultModelCapabilityConfig("minimax-video", "MiniMax-H3").video!;

    assert.deepEqual(normalizeVideoValue(profile, { seconds: "11", ratio: "16:9", resolution: "720" }), {
        seconds: "11",
        ratio: "16:9",
        resolution: "768P",
    });
});

test("TianYue defaults expose only documented protocol options and inherit the channel protocol", () => {
    const config = { channels: [{ id: "tianyue", models: ["configured-model"], interfaceType: "tianyue-video" }] };
    const video = modelCapabilityConfigFor(config, "tianyue::configured-model").video!;
    assert.deepEqual(video.ratios, ["16:9", "9:16", "4:3", "3:4", "1:1"]);
    assert.deepEqual(video.resolutions, ["480p", "720p", "1080p"]);
});

test("explicit per-model protocol and configured limits take precedence over the TianYue channel", () => {
    const profile = defaultModelCapabilityConfig("tianyue-video");
    profile.video!.duration = { selection: "enum", values: [20], default: 20 };
    profile.video!.references.maxImages = 12;
    const config = {
        channels: [
            {
                id: "tianyue",
                interfaceType: "tianyue-video",
                models: ["manual", "override"],
                modelCosts: [
                    { model: "manual", capabilityConfig: profile },
                    { model: "override", protocol: "minimax-video" },
                ],
            },
        ],
    };
    const manual = modelCapabilityConfigFor(config, "tianyue::manual").video!;
    assert.deepEqual(manual.duration.values, [20]);
    assert.equal(manual.references.maxImages, 12);
    assert.deepEqual(modelCapabilityConfigFor(config, "tianyue::override").video!.resolutions, ["768P", "2K"]);
});
