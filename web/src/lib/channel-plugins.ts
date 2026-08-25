import {
    aiStarsLabProtocolForCapability,
    applyAiStarsLabPreset,
    AISTARSLAB_BASE_URL,
    isAiStarsLabBaseUrl,
} from "@/lib/aistarslab-channel";
import {
    applyGlobalAiOpcPreset,
    GLOBALAIOPC_BASE_URL,
    GLOBALAIOPC_IMAGE_MODELS,
    GLOBALAIOPC_VIDEO_MODELS,
    isGlobalAiOpcBaseUrl,
} from "@/lib/globalaiopc-channel";
import {
    applyHuiQuYunPreset,
    HUIQUYUN_BASE_URL,
    huiQuYunProtocolForModel,
    isHuiQuYunBaseUrl,
    syncHuiQuYunModelCosts,
} from "@/lib/huiquyun-channel";
import { mergeFetchedChannelModelCosts, type ChannelModelCatalogItem } from "@/lib/channel-model-catalog";
import type { ModelProtocol } from "@/lib/model-protocols";
import type { ModelChannel } from "@/stores/use-config-store";

export type ChannelPluginId = "globalaiopc" | "huiquyun" | "aistarslab";

export type ChannelPluginMatchInput = Pick<ModelChannel, "baseUrl" | "connectionType"> & { interfaceType?: string };

export type ChannelPlugin = {
    id: ChannelPluginId;
    label: string;
    relayFormat: string;
    baseUrl: string;
    dedicated: boolean;
    allowedProtocols: readonly ModelProtocol[];
    matches: (channel: ChannelPluginMatchInput) => boolean;
    applyPreset: (channel: ModelChannel) => ModelChannel;
    protocolForModel?: (model: string, endpointTypes?: string[]) => ModelProtocol;
    syncModelCosts?: (channel: ModelChannel, models: string[], catalog?: ChannelModelCatalogItem[]) => NonNullable<ModelChannel["modelCosts"]>;
    staticCatalog?: () => ChannelModelCatalogItem[];
    validate?: (channel: ChannelPluginMatchInput) => string | undefined;
};

const HUIQUYUN_PROTOCOLS: readonly ModelProtocol[] = [
    "chat-completion",
    "openai-response",
    "openai-image",
    "openai-audio",
    "huiquyun-video",
];

const GLOBALAIOPC_PROTOCOLS: readonly ModelProtocol[] = ["globalaiopc-image", "globalaiopc-video"];
const AISTARSLAB_PROTOCOLS: readonly ModelProtocol[] = ["aistarslab-image", "aistarslab-video"];

const CHANNEL_PLUGINS: readonly ChannelPlugin[] = [
    {
        id: "globalaiopc",
        label: "GlobalAiOpc",
        relayFormat: "globalaiopc",
        baseUrl: GLOBALAIOPC_BASE_URL,
        dedicated: true,
        allowedProtocols: GLOBALAIOPC_PROTOCOLS,
        matches: (channel) => channel.connectionType === "globalaiopc" || channel.interfaceType === "globalaiopc-image" || channel.interfaceType === "globalaiopc-video" || isGlobalAiOpcBaseUrl(channel.baseUrl),
        applyPreset: applyGlobalAiOpcPreset,
        protocolForModel: (model) => (GLOBALAIOPC_IMAGE_MODELS as readonly string[]).includes(model.trim()) ? "globalaiopc-image" : "globalaiopc-video",
        staticCatalog: () => [
            ...GLOBALAIOPC_IMAGE_MODELS.map((id) => ({ id, supportedEndpointTypes: ["globalaiopc-image"] })),
            ...GLOBALAIOPC_VIDEO_MODELS.map((id) => ({ id, supportedEndpointTypes: ["globalaiopc-video"] })),
        ],
    },
    {
        id: "huiquyun",
        label: "汇取云",
        relayFormat: "openai",
        baseUrl: HUIQUYUN_BASE_URL,
        dedicated: true,
        allowedProtocols: HUIQUYUN_PROTOCOLS,
        matches: (channel) => channel.connectionType === "huiquyun" || channel.interfaceType === "huiquyun-video" || isHuiQuYunBaseUrl(channel.baseUrl),
        applyPreset: applyHuiQuYunPreset,
        protocolForModel: huiQuYunProtocolForModel,
        syncModelCosts: (channel, models, catalog) => syncHuiQuYunModelCosts(channel, models, catalog),
    },
    {
        id: "aistarslab",
        label: "AIStarsLab",
        relayFormat: "aistarslab",
        baseUrl: AISTARSLAB_BASE_URL,
        dedicated: true,
        allowedProtocols: AISTARSLAB_PROTOCOLS,
        matches: (channel) => channel.connectionType === "aistarslab" || channel.interfaceType === "aistarslab-image" || channel.interfaceType === "aistarslab-video" || isAiStarsLabBaseUrl(channel.baseUrl),
        applyPreset: applyAiStarsLabPreset,
        protocolForModel: (model, endpointTypes = []) => {
            const normalizedTypes = endpointTypes.map((value) => value.trim().toLowerCase());
            if (normalizedTypes.includes("aistarslab-image")) return aiStarsLabProtocolForCapability("image");
            if (normalizedTypes.includes("aistarslab-video")) return aiStarsLabProtocolForCapability("video");
            const normalizedModel = model.trim().toLowerCase();
            const imageMarkers = ["image", "seedream", "dall-e", "dalle", "flux", "imagen", "midjourney"];
            return imageMarkers.some((marker) => normalizedModel.includes(marker)) ? aiStarsLabProtocolForCapability("image") : aiStarsLabProtocolForCapability("video");
        },
        validate: (channel) => isAiStarsLabBaseUrl(channel.baseUrl) ? undefined : "AIStarsLab 渠道地址不正确",
    },
];

export function listChannelPlugins() {
    return CHANNEL_PLUGINS;
}

export function getChannelPluginById(id?: string) {
    return CHANNEL_PLUGINS.find((plugin) => plugin.id === id);
}

export function getChannelPlugin(channel: ChannelPluginMatchInput) {
    return CHANNEL_PLUGINS.find((plugin) => plugin.matches(channel));
}

export function channelPluginBaseUrls() {
    return CHANNEL_PLUGINS.map((plugin) => plugin.baseUrl);
}

export function channelPluginConnectionType(channel: ChannelPluginMatchInput) {
    return getChannelPlugin(channel)?.id;
}

export function channelPluginRelayFormat(channel: ChannelPluginMatchInput) {
    return getChannelPlugin(channel)?.relayFormat;
}

export function channelPluginAllowedProtocols(channel: ChannelPluginMatchInput) {
    return getChannelPlugin(channel)?.allowedProtocols;
}

export function channelPluginProtocolForModel(channel: ChannelPluginMatchInput, model: string, endpointTypes?: string[]) {
    return getChannelPlugin(channel)?.protocolForModel?.(model, endpointTypes);
}

export function mergeChannelPluginModelCosts(channel: ModelChannel, catalog: ChannelModelCatalogItem[]) {
    const plugin = getChannelPlugin(channel);
    if (plugin?.syncModelCosts) return plugin.syncModelCosts(channel, catalog.map((item) => item.id), catalog);
    return mergeFetchedChannelModelCosts(channel, catalog);
}

export function syncChannelPluginModelCosts(channel: ModelChannel, models: string[]) {
    return getChannelPlugin(channel)?.syncModelCosts?.(channel, models);
}

export function validateChannelPlugin(channel: ChannelPluginMatchInput) {
    return getChannelPlugin(channel)?.validate?.(channel);
}
