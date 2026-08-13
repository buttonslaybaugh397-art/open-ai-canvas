import { defaultModelCapabilityConfig } from "@/lib/model-capabilities";
import type { ModelChannel } from "@/stores/use-config-store";

export const GLOBALAIOPC_BASE_URL = "https://zcbservice.aizfw.cn/kyyReactApiServer";

export const GLOBALAIOPC_IMAGE_MODELS = ["seedream_5.0Pro", "seedream-5.0"] as const;

export const GLOBALAIOPC_VIDEO_MODELS = [
    "seedance-2.5-c1",
    "seedance-2.5-c2",
    "seedance-2.5-c3",
    "sd_2.0_fast_special",
    "sd_2.0_special",
    "sd_2.0_discount",
    "sd_2.0_fast_discount",
    "seedance_1_5_pro_1080p",
    "seedance_1_5_pro_720p",
    "seedance_1_5_pro_480p",
    "MiniMax-H3-c4",
    "videos_933_c1",
    "videos_fast_933_c1",
    "videos_stable",
    "videos_stable_fast",
] as const;

export const GLOBALAIOPC_MODELS = [...GLOBALAIOPC_IMAGE_MODELS, ...GLOBALAIOPC_VIDEO_MODELS];

export function isGlobalAiOpcBaseUrl(value: string) {
    return value.trim().replace(/\/+$/, "").toLowerCase() === GLOBALAIOPC_BASE_URL.toLowerCase();
}

export function globalAiOpcTaskUrl(baseUrl: string, taskId = "") {
    const base = baseUrl.trim().replace(/\/+$/, "");
    return `${base}/v2/model-center/tasks${taskId ? `/${encodeURIComponent(taskId)}` : ""}`;
}

export function globalAiOpcResponseCodeFailed(value: unknown) {
    if (value === undefined || value === null) return false;
    if (typeof value === "number") return value !== 0;
    if (typeof value === "string") return value.trim() !== "0";
    return true;
}

export function applyGlobalAiOpcPreset(channel: ModelChannel): ModelChannel {
    const existingCosts = new Map((channel.modelCosts || []).map((item) => [item.model, item]));
    const modelCosts: NonNullable<ModelChannel["modelCosts"]> = GLOBALAIOPC_MODELS.map((model) => {
        const protocol = (GLOBALAIOPC_IMAGE_MODELS as readonly string[]).includes(model) ? "globalaiopc-image" : "globalaiopc-video";
        const capability = protocol === "globalaiopc-image" ? "image" : "video";
        return {
            ...existingCosts.get(model),
            model,
            capability,
            protocol,
            billingMode: "fixed_request",
            unitPriceMicrocredits: 0,
            capabilityConfig: defaultModelCapabilityConfig(protocol, model),
        };
    });
    return {
        ...channel,
        name: channel.name.trim() && !/^渠道 \d+$/.test(channel.name.trim()) ? channel.name : "GlobalAiOpc",
        connectionType: "globalaiopc",
        apiFormat: "openai",
        interfaceType: undefined,
        baseUrl: GLOBALAIOPC_BASE_URL,
        models: [...GLOBALAIOPC_MODELS],
        modelCosts,
    };
}
