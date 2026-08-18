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

export function aiStarsLabModelRoute(config: ModelCapabilityConfig | undefined): AiStarsLabModelRoute | undefined {
    const route = config?.aistarslab;
    return route && typeof route.channel === "string" && route.channel.trim() ? route : undefined;
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
