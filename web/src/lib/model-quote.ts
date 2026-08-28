import { useEffect, useMemo, useState } from "react";

import { quoteLogicalModel, type LogicalModelQuote, type ModelRequestIntent } from "@/services/api/logical-models";
import { modelOptionName, resolveModelChannel, type AiConfig, type ModelCapability } from "@/stores/use-config-store";
import { modelRequestOptions, type ModelRequirements } from "@/lib/model-selection";

export type ModelQuoteRequest = {
    logicalModelID: string;
    intent: ModelRequestIntent;
};

export function modelQuoteRequest(config: AiConfig, value: string, capability?: ModelCapability, requirements?: ModelRequirements): ModelQuoteRequest | undefined {
    if (!capability || !value) return undefined;
    const channel = resolveModelChannel(config, value);
    if (channel.scope !== "system") return undefined;
    const cost = channel.modelCosts?.find((item) => item.model === modelOptionName(value));
    if (!cost?.logicalModelId) return undefined;
    const input = requirements?.input;
    const intent: ModelRequestIntent = {
        capability,
        operation: requirements?.videoOperation,
        inputs: {
            image: (input?.imageCount || 0) + (input?.characterCount || 0),
            video: input?.videoCount || 0,
            audio: input?.audioCount || 0,
        },
        options: {
            ...modelRequestOptions(config, capability),
            ...(requirements?.options || {}),
            ...(requirements?.videoSeconds ? { videoSeconds: Number(requirements.videoSeconds) } : {}),
            ...(requirements?.imageSize ? { size: requirements.imageSize } : {}),
        },
        ...(capability === "image" && requirements?.quoteQuantity && requirements.quoteQuantity > 0 ? { quantity: Math.floor(requirements.quoteQuantity) } : {}),
    };
    return { logicalModelID: cost.logicalModelId, intent };
}

export function fallbackLogicalModelCredits(config: AiConfig, value: string, capability?: ModelCapability, requirements?: ModelRequirements) {
    if (!capability || !value) return undefined;
    const channel = resolveModelChannel(config, value);
    const cost = channel.modelCosts?.find((item) => item.model === modelOptionName(value));
    const tiers = cost?.logicalPriceTiers || [];
    if (channel.scope !== "system" || !tiers.length) return undefined;
    const requested = quoteTierSelector(capability, requirements);
    let bestScore = -1;
    let tier: (typeof tiers)[number] | undefined;
    for (const candidate of tiers) {
        const selector = candidate.selector || {};
        const conditions = Object.entries(selector).filter(([, candidateValue]) => candidateValue && candidateValue !== "*");
        if (conditions.some(([name, candidateValue]) => requested[name] !== candidateValue)) continue;
        const score = conditions.length;
        if (score > bestScore) {
            bestScore = score;
            tier = candidate;
        }
    }
    if (!tier || tier.billingMode === "token" || tier.unitPriceMicrocredits <= 0) return undefined;
    const quantity = tier.billingMode === "per_second"
        ? Math.max(1, Math.floor(Math.abs(Number(requirements?.videoSeconds)) || 1))
        : Math.max(1, Math.floor(requirements?.quoteQuantity || 1));
    return (tier.unitPriceMicrocredits * quantity) / 1_000_000;
}

function quoteTierSelector(capability: ModelCapability, requirements?: ModelRequirements) {
    const selector: Record<string, string> = {};
    const input = requirements?.input;
    if (capability === "video") {
        const imageCount = (input?.imageCount || 0) + (input?.characterCount || 0);
        if ((input?.videoCount || 0) > 0) selector.operation = "video_to_video";
        else if (imageCount > 0) selector.operation = "image_to_video";
        else if (requirements?.videoOperation) selector.operation = requirements.videoOperation;
        if (imageCount > 0) selector.imageCount = String(imageCount);
        const resolution = normalizeResolution(requirements?.options?.vquality);
        if (resolution) selector.vquality = resolution;
        if (requirements?.videoSeconds) selector.videoSeconds = String(Math.floor(Number(requirements.videoSeconds)));
    } else if (capability === "image") {
        for (const name of ["quality", "size"]) {
            const value = String(requirements?.options?.[name] || "").trim().toLowerCase();
            if (value && value !== "auto" && value !== "any") selector[name] = value;
        }
    }
    return selector;
}

function normalizeResolution(value: unknown) {
    const normalized = String(value || "").trim().toLowerCase();
    if (!normalized || normalized === "auto" || normalized === "default") return "";
    if (normalized === "low") return "480p";
    if (normalized === "medium" || normalized === "high") return "720p";
    if (normalized === "2k") return "1440p";
    if (normalized === "4k") return "2160p";
    return normalized.endsWith("p") ? normalized : `${normalized}p`;
}

export function useLogicalModelQuote(
    config: AiConfig,
    value: string,
    capability?: ModelCapability,
    requirements?: ModelRequirements,
    enabled = true,
) {
    const request = useMemo(() => modelQuoteRequest(config, value, capability, requirements), [capability, config, requirements, value]);
    const requestKey = request ? `${request.logicalModelID}:${JSON.stringify(request.intent)}` : "";
    const [quote, setQuote] = useState<LogicalModelQuote>();

    useEffect(() => {
        if (!enabled || !request) {
            setQuote(undefined);
            return;
        }
        const controller = new AbortController();
        setQuote(undefined);
        quoteLogicalModel(request.logicalModelID, request.intent, controller.signal)
            .then((payload) => setQuote(payload.quote))
            .catch(() => {
                if (!controller.signal.aborted) setQuote(undefined);
            });
        return () => controller.abort();
    }, [enabled, requestKey]);

    return quote;
}
