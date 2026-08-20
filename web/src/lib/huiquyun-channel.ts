import { defaultModelCapabilityConfig } from "@/lib/model-capabilities";
import { isHuiQuYunVideoModel } from "@/lib/huiquyun-models";
import { modelProtocolCapability, protocolForModelCatalog, type ModelProtocol } from "@/lib/model-protocols";
import type { ModelChannel } from "@/stores/use-config-store";

export const HUIQUYUN_BASE_URL = "https://api.bjhuiqu.net/v1";

type HuiQuYunCatalogItem = { id: string; supportedEndpointTypes?: string[] };

export function isHuiQuYunBaseUrl(value: string) {
    const normalized = value.trim().replace(/\/+$/, "").toLowerCase();
    return normalized === "https://api.bjhuiqu.net" || normalized === HUIQUYUN_BASE_URL.toLowerCase();
}

export function huiQuYunProtocolForModel(model: string, endpointTypes: string[] = []): ModelProtocol {
    const catalogProtocol = protocolForModelCatalog(endpointTypes);
    if (modelProtocolCapability(catalogProtocol) === "video") return "huiquyun-video";
    if (catalogProtocol) return catalogProtocol;

    const normalized = model.trim().toLowerCase();
    if (matchesAny(normalized, ["tts", "speech", "voice", "audio", "music", "sound"])) return "openai-audio";
    if (isHuiQuYunVideoModel(normalized)) return "huiquyun-video";
    if (matchesAny(normalized, ["gpt-image", "nano-banana", "nanobanana", "seedream", "image", "dall-e", "dalle", "imagen", "flux", "sdxl", "stable-diffusion", "midjourney", "ideogram", "recraft"])) return "openai-image";
    return "chat-completion";
}

export function syncHuiQuYunModelCosts(channel: ModelChannel, models: string[], catalog: HuiQuYunCatalogItem[] = []): NonNullable<ModelChannel["modelCosts"]> {
    const existingByModel = new Map((channel.modelCosts || []).map((item) => [item.model, item]));
    const catalogByModel = new Map(catalog.map((item) => [item.id, item.supportedEndpointTypes || []]));
    return models.map((model) => {
        const existing = existingByModel.get(model);
        const protocol = existing?.protocol || huiQuYunProtocolForModel(model, catalogByModel.get(model));
        const capability = modelProtocolCapability(protocol) || "text";
        const capabilityConfig = capability === "image" || capability === "video"
            ? existing?.protocol === protocol && existing.capabilityConfig
                ? existing.capabilityConfig
                : defaultModelCapabilityConfig(protocol, model)
            : undefined;
        return {
            ...existing,
            model,
            capability,
            protocol,
            billingMode: existing?.billingMode || "fixed_request",
            unitPriceMicrocredits: existing?.unitPriceMicrocredits || 0,
            capabilityConfig,
        };
    });
}

export function applyHuiQuYunPreset(channel: ModelChannel): ModelChannel {
    const models = Array.from(new Set(channel.models.map((model) => model.trim()).filter(Boolean)));
    const preset = {
        ...channel,
        name: channel.name.trim() && !/^渠道 \d+$/.test(channel.name.trim()) ? channel.name : "汇取云",
        connectionType: "huiquyun" as const,
        apiFormat: "openai" as const,
        interfaceType: undefined,
        baseUrl: HUIQUYUN_BASE_URL,
        allowLocalChannel: false,
        models,
    };
    return { ...preset, modelCosts: syncHuiQuYunModelCosts(preset, models) };
}

function matchesAny(model: string, markers: string[]) {
    return markers.some((marker) => model.includes(marker));
}
