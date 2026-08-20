import { describe, expect, test } from "bun:test";

import { huiQuYunProtocolForModel } from "../src/lib/huiquyun-channel";
import { defaultModelCapabilityConfig } from "../src/lib/model-capabilities";
import { normalizeVideoResolution, VIDEO_RESOLUTION_CAPABILITY_OPTIONS, VIDEO_RESOLUTION_OPTIONS } from "../src/lib/video-generation-options";

describe("video generation resolution options", () => {
    test("统一档位包含 1440P 与 4K，并识别常见别名", () => {
        expect(VIDEO_RESOLUTION_OPTIONS).toEqual([480, 720, 1080, 1440, 2160]);
        expect(VIDEO_RESOLUTION_CAPABILITY_OPTIONS).toEqual(["480p", "720p", "1080p", "1440p", "2160p"]);
        expect(normalizeVideoResolution("2k")).toBe("1440");
        expect(normalizeVideoResolution("1440p")).toBe("1440");
        expect(normalizeVideoResolution("4K")).toBe("2160");
    });

    test("保留模型声明的非标准档位而不是静默降级", () => {
        expect(normalizeVideoResolution("768p")).toBe("768");
    });

    test("按协议限制实际可选档位", () => {
        expect(defaultModelCapabilityConfig("newapi-channel-2").video?.resolutions).toEqual(["480p", "720p", "1080p", "1440p", "2160p"]);
        expect(defaultModelCapabilityConfig("volcengine-ark-video").video?.resolutions).toEqual(["480p", "720p", "1080p"]);
        expect(defaultModelCapabilityConfig("volcengine-jimeng-video").video?.resolutions).toEqual(["720p"]);
        expect(defaultModelCapabilityConfig("gemini-veo").video?.resolutions).toEqual(["720p", "1080p"]);
    });

    test("汇取云按模型名自动识别专用协议且不改变通用协议目录", () => {
        expect(huiQuYunProtocolForModel("gpt-4.1-mini")).toBe("chat-completion");
        expect(huiQuYunProtocolForModel("gpt-image-1")).toBe("openai-image");
        expect(huiQuYunProtocolForModel("gpt-4o-mini-tts")).toBe("openai-audio");
        expect(huiQuYunProtocolForModel("sora-2-pro-15s")).toBe("huiquyun-video");
        expect(huiQuYunProtocolForModel("sd2-mx933-720-5s")).toBe("huiquyun-video");
        expect(huiQuYunProtocolForModel("sd2-mx933-720-fast-5s")).toBe("huiquyun-video");
        expect(huiQuYunProtocolForModel("catalog-video", ["openai-video"])).toBe("huiquyun-video");
    });

    test("汇取云固定时长视频模型锁定文档声明的参数", () => {
        const profile = defaultModelCapabilityConfig("huiquyun-video", "sora-2-pro-15s").video;

        expect(profile?.duration).toEqual({ selection: "enum", values: [15], default: 15 });
        expect(profile?.resolutions).toEqual(["720p"]);
        expect(profile?.references).toMatchObject({ maxImages: 4, maxVideos: 3, maxAudios: 1 });
    });

    test("汇取云 MX933 使用官方多媒体能力", () => {
        const profile = defaultModelCapabilityConfig("huiquyun-video", "sd2-mx933-720-10s").video;

        expect(profile?.duration).toEqual({ selection: "enum", values: [10], default: 10 });
        expect(profile?.resolutions).toEqual(["480p", "720p"]);
        expect(profile?.ratios).toEqual(["16:9", "9:16", "1:1", "4:3", "3:4", "3:2", "2:3"]);
        expect(profile?.references).toMatchObject({ maxImages: 9, maxVideos: 3, maxAudios: 3, maxVideoBytes: 50 * 1024 * 1024 });
    });
});
