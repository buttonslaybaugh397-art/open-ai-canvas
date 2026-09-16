import { defaultModelCapabilityConfig } from "@/lib/model-capabilities";
import { modelProtocolDefinition, type ModelProtocolDefinition, type ProtocolCapability } from "@/lib/model-protocols";
import type { ModelChannel } from "@/stores/use-config-store";

type ModelCost = NonNullable<ModelChannel["modelCosts"]>[number];
type ModelSettingsPatch = Partial<Pick<ModelCost, "capability" | "protocol" | "capabilityConfig">>;

export function resolveChannelModelSettings(channel: ModelChannel, model: string, protocols: ModelProtocolDefinition[]) {
    const cost = channel.modelCosts?.find((item) => item.model === model);
    const protocol = cost?.protocol || channel.interfaceType;
    const definition = modelProtocolDefinition(protocol, protocols);
    const capability = cost?.capability || definition?.capability || inferCapabilityFromModel(model);
    const availableProtocol = definition?.enabled !== false && definition?.capability === capability ? definition : undefined;
    let capabilityConfig = cost?.capabilityConfig;
    if (capability !== "audio" && !capabilityConfig?.[capability]) {
        const defaults = defaultModelCapabilityConfig(protocol, model);
        capabilityConfig = { ...capabilityConfig, version: capabilityConfig?.version ?? defaults.version, [capability]: defaults[capability] };
    }
    return { cost, protocol, capability, availableProtocol, capabilityConfig };
}

export function updateChannelModelSettings(channel: ModelChannel, model: string, patch: ModelSettingsPatch, protocols: ModelProtocolDefinition[]): ModelCost[] {
    if (!channel.models.includes(model)) throw new Error("当前模型已不在渠道中");
    const { cost, capability } = resolveChannelModelSettings(channel, model, protocols);
    const current: ModelCost = cost || { model, capability, billingMode: "fixed_request", unitPriceMicrocredits: 0 };
    const next = {
        ...current,
        ...patch,
        model,
        // Parameter editors emit one capability at a time; preserve the others.
        ...(patch.capabilityConfig ? { capabilityConfig: { ...current.capabilityConfig, ...patch.capabilityConfig } } : {}),
    };
    if (patch.protocol !== undefined && !protocols.some((item) => item.value === patch.protocol && item.capability === next.capability && item.enabled !== false)) {
        throw new Error("请选择当前能力下已启用的请求协议");
    }
    const costs = cost ? (channel.modelCosts || []).map((item) => (item.model === model ? next : item)) : [...(channel.modelCosts || []), next];
    return costs.filter((item) => channel.models.includes(item.model));
}

function inferCapabilityFromModel(model: string): ProtocolCapability {
    const lower = model.toLowerCase();
    if (
        lower.includes("seedream") ||
        lower.includes("image") ||
        lower.includes("dall-e") ||
        lower.includes("dalle") ||
        lower.includes("flux") ||
        lower.includes("imagen") ||
        lower.includes("banana") ||
        lower.includes("midjourney") ||
        lower.includes("sdxl") ||
        lower.includes("stable-diffusion")
    ) {
        return "image";
    }
    if (
        lower.includes("video") ||
        lower.includes("sora") ||
        lower.includes("veo") ||
        lower.includes("kling") ||
        lower.includes("seedance") ||
        lower.includes("minimax") ||
        lower.includes("hailuo") ||
        lower.includes("pika") ||
        lower.includes("runway") ||
        lower.includes("omni") ||
        lower.includes("cogvideo") ||
        lower.includes("wan")
    ) {
        return "video";
    }
    if (lower.includes("audio") || lower.includes("tts") || lower.includes("voice") || lower.includes("speech") || lower.includes("sound") || lower.includes("music")) {
        return "audio";
    }
    return "text";
}
