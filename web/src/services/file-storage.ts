import localforage from "localforage";
import { nanoid } from "nanoid";

import { getActiveUserScope } from "@/lib/user-scope";
import { getResourceBlob, resourceFileUrl, resourceIdFromStorageKey, resourceIdFromUrl, resourceProxyFileUrl, resourceStorageKey, uploadResourceFile } from "@/services/api/resources";
import { getCachedResourceBlob, getCachedResourceObjectUrl, primeResourceBlobCache } from "@/services/resource-blob-cache";

export type UploadedFile = { url: string; storageKey: string; bytes: number; mimeType: string; width?: number; height?: number; durationMs?: number };

const store = localforage.createInstance({ name: "infinite-canvas", storeName: "media_files" });
const objectUrls = new Map<string, string>();

export async function uploadMediaFile(input: string | Blob, prefix = "file", options?: { allowLocalFallback?: boolean }): Promise<UploadedFile> {
    const blob = typeof input === "string" ? await (await fetch(input)).blob() : input;
    const previewUrl = URL.createObjectURL(blob);
    const meta: { width?: number; height?: number; durationMs?: number } = blob.type.startsWith("video/") ? await readVideoMeta(previewUrl) : blob.type.startsWith("audio/") ? await readAudioMeta(previewUrl) : {};
    const allowLocalFallback = options?.allowLocalFallback ?? !blob.type.startsWith("video/");
    try {
        const kind = blob.type.startsWith("video/") ? "video" : blob.type.startsWith("audio/") ? "audio" : "file";
        const resource = await uploadResourceFile(blob, kind, { ...meta, fileName: input instanceof File ? input.name : undefined });
        await primeResourceBlobCache(resourceStorageKey(resource.id), blob).catch(() => "");
        URL.revokeObjectURL(previewUrl);
        return {
            url: resource.publicUrl || resourceFileUrl(resource.id),
            storageKey: resourceStorageKey(resource.id),
            bytes: resource.size || blob.size,
            mimeType: resource.mimeType || blob.type || "application/octet-stream",
            width: resource.width || meta.width,
            height: resource.height || meta.height,
            durationMs: resource.durationMs || meta.durationMs,
        };
    } catch (error) {
        if (!allowLocalFallback) {
            URL.revokeObjectURL(previewUrl);
            throw error instanceof Error ? error : new Error("媒体同步到服务器资源存储失败");
        }
        // OSS is optional during local/self-hosted setup. Keep the existing local fallback.
    }
    const storageKey = `${prefix}:${getActiveUserScope()}:${nanoid()}`;
    await store.setItem(storageKey, blob);
    const url = previewUrl;
    objectUrls.set(storageKey, url);
    return { url, storageKey, bytes: blob.size, mimeType: blob.type || "application/octet-stream", ...meta };
}

export async function resolveMediaUrl(storageKey?: string, fallback = "") {
    if (!storageKey) return fallback;
    const resourceId = resourceIdFromStorageKey(storageKey);
    if (resourceId) {
        // Keep the provider URL primary; the resource is the durable fallback.
        if (fallback) return fallback;
        const cached = await getCachedResourceObjectUrl(storageKey).catch(() => "");
        return cached || resourceFileUrl(resourceId);
    }
    const cached = objectUrls.get(storageKey);
    if (cached) return cached;
    const blob = await store.getItem<Blob>(storageKey);
    if (!blob) return fallback;
    const url = URL.createObjectURL(blob);
    objectUrls.set(storageKey, url);
    return url;
}

/** Resolve a resource to a browser-readable same-origin URL for media tools. */
export function resolveReadableMediaUrl(storageKey?: string) {
    const resourceId = resourceIdFromStorageKey(storageKey);
    return resourceId ? resourceProxyFileUrl(resourceId) : "";
}

export async function getMediaBlob(storageKey: string) {
    if (resourceIdFromStorageKey(storageKey)) return getCachedResourceBlob(storageKey);
    return store.getItem<Blob>(storageKey);
}

/** Read media for operations that need response bytes, using the same-origin resource proxy. */
export async function getMediaBlobFromSource(source?: string, storageKey?: string) {
    if (storageKey) {
        const stored = await getMediaBlob(storageKey);
        if (stored) return stored;
        if (resourceIdFromStorageKey(storageKey)) return null;
    }
    const resourceId = resourceIdFromUrl(source);
    if (resourceId) return getResourceBlob(resourceStorageKey(resourceId));
    if (!source) return null;
    const response = await fetch(source, { credentials: "same-origin" });
    if (!response.ok) throw new Error(`媒体资源请求失败（${response.status}）`);
    return response.blob();
}

export async function setMediaBlob(storageKey: string, blob: Blob) {
    if (resourceIdFromStorageKey(storageKey)) return primeResourceBlobCache(storageKey, blob);
    await store.setItem(storageKey, blob);
    const url = URL.createObjectURL(blob);
    objectUrls.set(storageKey, url);
    return url;
}

export async function deleteStoredMedia(keys: Iterable<string>) {
    await Promise.all(
        Array.from(new Set(keys)).map(async (key) => {
            if (resourceIdFromStorageKey(key)) return;
            const url = objectUrls.get(key);
            if (url) URL.revokeObjectURL(url);
            objectUrls.delete(key);
            await store.removeItem(key);
        }),
    );
}

export async function cleanupUnusedMedia(usedData: unknown, scope = getActiveUserScope()) {
    const usedKeys = collectMediaStorageKeys(usedData);
    const currentScope = scope;
    const unused: string[] = [];
    await store.iterate((_value, key) => {
        const parts = key.split(":");
        if (parts.length >= 3 && parts[1] === currentScope && !usedKeys.has(key)) unused.push(key);
    });
    await Promise.all(unused.map((key) => store.removeItem(key)));
}

export function collectMediaStorageKeys(value: unknown, keys = new Set<string>()) {
    if (!value || typeof value !== "object") return keys;
    if ("storageKey" in value && typeof value.storageKey === "string" && (value.storageKey.includes(":") || resourceIdFromStorageKey(value.storageKey))) keys.add(value.storageKey);
    Object.values(value).forEach((item) => (Array.isArray(item) ? item.forEach((child) => collectMediaStorageKeys(child, keys)) : collectMediaStorageKeys(item, keys)));
    return keys;
}

function readVideoMeta(url: string) {
    return new Promise<{ width: number; height: number; durationMs?: number }>((resolve) => {
        const video = document.createElement("video");
        const done = () => resolve({ width: video.videoWidth || 1280, height: video.videoHeight || 720, durationMs: Number.isFinite(video.duration) ? Math.round(video.duration * 1000) : undefined });
        video.onloadedmetadata = done;
        video.onerror = done;
        video.src = url;
    });
}

function readAudioMeta(url: string) {
    return new Promise<{ durationMs?: number }>((resolve) => {
        const audio = document.createElement("audio");
        const done = () => resolve({ durationMs: Number.isFinite(audio.duration) ? Math.round(audio.duration * 1000) : undefined });
        audio.onloadedmetadata = done;
        audio.onerror = done;
        audio.src = url;
    });
}
