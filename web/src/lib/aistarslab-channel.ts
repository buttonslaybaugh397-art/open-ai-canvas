import { defaultModelCapabilityConfig, type ModelCapabilityConfig } from "@/lib/model-capabilities";
import type { ModelProtocol } from "@/lib/model-protocols";
import type { ModelChannel } from "@/stores/use-config-store";

export const AISTARSLAB_BASE_URL = "https://api.video.aistarslab.com/openapi";

export type AiStarsLabModelRoute = {
    channel: string;
    capability: "image" | "video";
    model: string;
    label?: string;
    qualities?: string[];
    aspectRatios?: string[];
    duration?: number[];
    modes?: string[];
    inputImagesMax?: number;
    inputVideosMax?: number;
    inputAudiosMax?: number;
};

export function isAiStarsLabBaseUrl(value: string) {
    return value.trim().replace(/\/+$/, "").toLowerCase() === AISTARSLAB_BASE_URL;
}

export function aiStarsLabModelRoute(config: ModelCapabilityConfig | undefined, modelKey: string, capability: "image" | "video"): AiStarsLabModelRoute | undefined {
    const route = config?.aistarslab;
    if (route && typeof route.channel === "string" && route.channel.trim()) return route;
    // 早期存量记录把线路编码写进了 modelKey（`<线路>:<模型>`）且没有线路块；这类模型仍可能已定价启用并被用户选中，
    // 生成不能卡在“先去后台重新拉取模型”上，因此线路块缺失时按 modelKey 现场还原。
    // 还原结果只保证 channel 与 model 可信，参考素材上限保持未声明并交由上游校验。
    const key = String(modelKey || "").trim();
    const separator = key.indexOf(":");
    const channel = separator < 0 ? "" : key.slice(0, separator).trim();
    const model = separator < 0 ? "" : key.slice(separator + 1).trim();
    if (!channel || !model) return undefined;
    return { ...route, channel, model, capability: route?.capability ?? capability, inputImagesMax: undefined, inputVideosMax: undefined, inputAudiosMax: undefined };
}

export function aiStarsLabProtocolForCapability(capability: "image" | "video"): ModelProtocol {
    return capability === "image" ? "aistarslab-image" : "aistarslab-video";
}

export function applyAiStarsLabPreset(channel: ModelChannel): ModelChannel {
    const models = Array.from(new Set(channel.models.map((model) => model.trim()).filter(Boolean)));
    return {
        ...channel,
        name: channel.name.trim() && !/^渠道 \d+$/.test(channel.name.trim()) ? channel.name : "AIStarsLab",
        connectionType: "aistarslab",
        apiFormat: "openai",
        interfaceType: undefined,
        baseUrl: AISTARSLAB_BASE_URL,
        allowLocalChannel: false,
        models,
    };
}

export function defaultAiStarsLabCapability(protocol: ModelProtocol, model: string): ModelCapabilityConfig {
    const config = defaultModelCapabilityConfig(protocol, model);
    if (protocol === "aistarslab-image") {
        config.image!.references.maskSupported = false;
        config.image!.size.parameter = "aspect_ratio";
        config.image!.size.allowCustom = false;
        config.image!.maxOutputs = 1;
    }
    return config;
}

export function withAiStarsLabRoute(config: ModelCapabilityConfig, route: AiStarsLabModelRoute): ModelCapabilityConfig {
    return { ...config, aistarslab: route };
}
