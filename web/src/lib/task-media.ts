import { useSyncExternalStore } from "react";

export type TaskMediaSource = {
    url: string;
    storageKey?: string;
    kind?: "image" | "video" | "audio";
};

type TaskMediaSourceTask = {
    previewUrl?: string;
    previewKind?: "image" | "video";
    previewStorageKey?: string;
    resultJson?: string;
    outputs?: Array<{ providerArtifactRef?: string }>;
};

const mediaUrlFields = ["url", "videoUrl", "imageUrl", "outputUrl", "mediaUrl", "dataUrl"] as const;
const resolvedVideoFallbacks = new Map<string, string>();
const fallbackListeners = new Set<() => void>();

export function getResolvedVideoFallbackUrl(url: string) {
    return resolvedVideoFallbacks.get(url) || "";
}

export function setResolvedVideoFallbackUrl(sourceUrl: string, fallbackUrl: string) {
    if (!sourceUrl || !fallbackUrl || sourceUrl === fallbackUrl || resolvedVideoFallbacks.get(sourceUrl) === fallbackUrl) return;
    resolvedVideoFallbacks.set(sourceUrl, fallbackUrl);
    fallbackListeners.forEach((listener) => listener());
}

export function subscribeResolvedVideoFallbacks(listener: () => void) {
    fallbackListeners.add(listener);
    return () => fallbackListeners.delete(listener);
}

export function useResolvedVideoFallbackUrl(sourceUrl: string) {
    return useSyncExternalStore(
        subscribeResolvedVideoFallbacks,
        () => getResolvedVideoFallbackUrl(sourceUrl),
        () => "",
    );
}

export function parseTaskMediaSources(value?: string): TaskMediaSource[] {
    if (!value) return [];
    let parsed: unknown;
    try {
        parsed = JSON.parse(value);
    } catch {
        return isMediaUrl(value) ? [{ url: value }] : [];
    }

    const sources: TaskMediaSource[] = [];
    const visit = (item: unknown, context = "") => {
        if (typeof item === "string") {
            if (isMediaUrl(item) && isMediaContext(context)) {
                const kind = mediaKind(context);
                addSource(sources, { url: item, ...(kind ? { kind } : {}) });
            }
            return;
        }
        if (Array.isArray(item)) {
            item.forEach((entry) => visit(entry, context));
            return;
        }
        if (!item || typeof item !== "object") return;

        const record = item as Record<string, unknown>;
        const kind = mediaKind([context, record.mimeType, record.type, record.mode].filter((part) => typeof part === "string").join(" "));
        const storageKey = typeof record.storageKey === "string" ? record.storageKey : undefined;
        for (const field of mediaUrlFields) {
            const url = record[field];
            if (typeof url === "string" && isMediaUrl(url) && (kind || isMediaContext(field))) {
                addSource(sources, { url, ...(storageKey ? { storageKey } : {}), ...(kind ? { kind } : {}) });
                break;
            }
        }
        Object.entries(record).forEach(([field, child]) => {
            // A media record can expose its provider URL and durable resource URL together.
            // The selected URL above is the single display source; do not add its backup again.
            if (typeof child === "string" && mediaUrlFields.includes(field as (typeof mediaUrlFields)[number])) return;
            visit(child, field);
        });
    };

    visit(parsed);
    return sources;
}

export function taskPreviewSource(task: TaskMediaSourceTask): TaskMediaSource | undefined {
    if (!task.previewUrl) return undefined;
    const parsedSources = parseTaskMediaSources(task.resultJson);
    const matching = parsedSources.find((source) => source.url === task.previewUrl);
    const storageKey = task.previewStorageKey || matching?.storageKey || parsedSources.find((source) => source.kind === "video" && source.storageKey)?.storageKey || task.outputs?.find((output) => output.providerArtifactRef?.startsWith("resource:"))?.providerArtifactRef;
    return { url: task.previewUrl, ...(storageKey ? { storageKey } : {}), ...(task.previewKind ? { kind: task.previewKind } : {}) };
}

function addSource(sources: TaskMediaSource[], source: TaskMediaSource) {
    const existing = sources.find((candidate) => candidate.url === source.url);
    if (!existing) {
        sources.push(source);
        return;
    }
    if (!existing.storageKey && source.storageKey) existing.storageKey = source.storageKey;
    if (!existing.kind && source.kind) existing.kind = source.kind;
}

function isMediaUrl(value: string) {
    return /^(data:(?:image|video|audio)\/|https?:|blob:|\/)/i.test(value);
}

function isMediaContext(value: string) {
    return /(image|video|audio|media|result|output|url|data)/i.test(value);
}

function mediaKind(value: string): TaskMediaSource["kind"] | undefined {
    if (/video/i.test(value) || /data:video\//i.test(value) || /\.(mp4|webm|mov)(?:$|[?#])/i.test(value)) return "video";
    if (/audio/i.test(value) || /data:audio\//i.test(value) || /\.(mp3|wav|m4a|ogg)(?:$|[?#])/i.test(value)) return "audio";
    if (/image/i.test(value) || /data:image\//i.test(value) || /\.(png|jpe?g|webp|gif|avif)(?:$|[?#])/i.test(value)) return "image";
    return undefined;
}
