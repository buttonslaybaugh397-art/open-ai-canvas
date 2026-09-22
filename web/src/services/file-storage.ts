import localforage from "localforage";
import { nanoid } from "nanoid";

import { getActiveUserScope } from "@/lib/user-scope";
import { captureVideoPoster, detectVideoAudioTrackFromBlob } from "@/lib/video-poster";
import { resourceFileUrl, resourceIdFromStorageKey, resourceStorageKey, ResourceUploadError, uploadResourceFile } from "@/services/api/resources";
import { uploadImage, type UploadedImage } from "@/services/image-storage";
import { cacheResourceObjectUrl, cacheResourceObjectUrlDurably, getCachedResourceBlob, primeResourceBlobCache } from "@/services/resource-blob-cache";

export type UploadedFile = {
    url: string;
    storageKey: string;
    bytes: number;
    mimeType: string;
    width?: number;
    height?: number;
    durationMs?: number;
    hasAudio?: boolean;
    preview?: UploadedImage;
    /**
     * true 表示直传失败、文件当前只存在于本机 IndexedDB。语义与 UploadedImage 一致：
     * 云端数据同步会用同一幂等键重传，但在那之前 `url` 是页面级 objectURL，刷新即失效。
     */
    pendingRemoteUpload?: boolean;
    /** 直传失败原因，仅在 pendingRemoteUpload 为 true 时有值。 */
    remoteUploadError?: string;
};

const store = localforage.createInstance({ name: "infinite-canvas", storeName: "media_files" });
const objectUrls = new Map<string, string>();

export async function uploadMediaFile(input: Blob, prefix = "file", onProgress?: (uploadedBytes: number, totalBytes: number) => void): Promise<UploadedFile> {
    // 直传和失败后的本地同步必须复用同一上传身份，避免响应丢失后创建第二个对象。
    const storageKey = `${prefix}:${getActiveUserScope()}:${nanoid()}`;
    const blob = input;
    const previewUrl = URL.createObjectURL(blob);
    let retainPreviewUrl = false;
    let posterUpload: Promise<UploadedImage | undefined> = Promise.resolve(undefined);

    try {
        let captured: Awaited<ReturnType<typeof captureVideoPoster>> | undefined;
        if (blob.type.startsWith("video/")) {
            try {
                captured = await captureVideoPoster(previewUrl);
            } catch (error) {
                // 封面和轨道信息属于展示增强：失败不阻断原文件上传，但必须留下可诊断信号。
                console.warn("读取视频封面与媒体信息失败，继续上传原文件", { mimeType: blob.type, bytes: blob.size, error });
            }
        }

        // 浏览器轨道探测对部分 MP4/MOV 会误报；只有未确认存在音轨时才做二次解析，
        // 避免正常上传重复读取整个文件。
        let parsedHasAudio: boolean | undefined;
        if (blob.type.startsWith("video/") && captured?.hasAudio !== true) {
            try {
                parsedHasAudio = await detectVideoAudioTrackFromBlob(blob);
            } catch (error) {
                console.warn("解析视频音轨失败，继续上传但不写入音轨结论", { mimeType: blob.type, bytes: blob.size, error });
            }
        }
        const resolvedHasAudio = parsedHasAudio ?? (captured?.hasAudio === false ? undefined : captured?.hasAudio);

        let meta: { width?: number; height?: number; durationMs?: number; hasAudio?: boolean };
        if (captured) {
            meta = { width: captured.width, height: captured.height, durationMs: captured.durationMs, hasAudio: resolvedHasAudio };
        } else if (blob.type.startsWith("audio/")) {
            try {
                meta = await readAudioMeta(previewUrl);
            } catch (error) {
                console.warn("读取音频时长失败，继续上传原文件", { mimeType: blob.type, bytes: blob.size, error });
                meta = {};
            }
        } else {
            meta = { hasAudio: resolvedHasAudio };
        }

        // 封面与视频本体并行传输，避免对象存储延迟在两个请求之间累加。
        posterUpload = captured?.poster ? uploadImage(captured.poster).catch((error) => {
            console.warn("上传视频预览图失败，继续保存视频本体", { mimeType: blob.type, bytes: blob.size, error });
            return undefined;
        }) : Promise.resolve(undefined);

        let remoteUploadError = "";
        try {
            const kind = blob.type.startsWith("video/") ? "video" : blob.type.startsWith("audio/") ? "audio" : "file";
            const resource = await uploadResourceFile(blob, kind, { ...meta, fileName: input instanceof File ? input.name : undefined, idempotencyKey: storageKey }, onProgress);
            const poster = await posterUpload;
            try {
                await primeResourceBlobCache(resourceStorageKey(resource.id), blob);
            } catch (error) {
                // 缓存只影响后续读取性能，服务端资源已经成功落盘，不得把缓存失败误报为上传失败。
                console.warn("预热媒体缓存失败，服务端资源已保存", { resourceId: resource.id, error });
            }
            return { url: resource.publicUrl || resourceFileUrl(resource.id), storageKey: resourceStorageKey(resource.id), bytes: resource.size || blob.size, mimeType: resource.mimeType || blob.type || "application/octet-stream", width: resource.width || meta.width, height: resource.height || meta.height, durationMs: resource.durationMs || meta.durationMs, hasAudio: meta.hasAudio, preview: poster };
        } catch (error) {
            // 与图片上传同一套判定：永久性失败必须当场暴露，不能混进“稍后自动同步”。
            if (error instanceof ResourceUploadError && error.permanent) throw error;
            remoteUploadError = error instanceof Error ? error.message : "媒体直传失败";
        }

        // 瞬时失败退回本机：文件仍可用，且云端数据同步会用同一幂等键重传。
        await store.setItem(storageKey, blob);
        retainPreviewUrl = true;
        objectUrls.set(storageKey, previewUrl);
        const poster = await posterUpload;
        return { url: previewUrl, storageKey, bytes: blob.size, mimeType: blob.type || "application/octet-stream", ...meta, preview: poster, pendingRemoteUpload: true, remoteUploadError };
    } finally {
        // 异常路径也收拢并行封面请求，避免调用结束后仍继续写入。
        await posterUpload;
        // 只有本地降级结果需要把 objectURL 留给页面；成功上传和所有异常路径都及时释放。
        if (!retainPreviewUrl) URL.revokeObjectURL(previewUrl);
    }
}

export async function resolveMediaUrl(storageKey?: string, fallback = "") {
    if (!storageKey) return fallback;
    const resourceId = resourceIdFromStorageKey(storageKey);
    if (resourceId) {
        return cacheResourceObjectUrl(storageKey);
    }
    const cached = objectUrls.get(storageKey);
    if (cached) return cached;
    const blob = await store.getItem<Blob>(storageKey);
    if (!blob) return fallback;
    const url = URL.createObjectURL(blob);
    objectUrls.set(storageKey, url);
    return url;
}

/**
 * Resolve a video source without buffering a remote resource into a Blob first.
 * The resource endpoint redirects to the signed CDN URL, so the browser can
 * request metadata and byte ranges incrementally.
 */
export async function resolveVideoPlaybackUrl(storageKey?: string, fallback = "") {
    if (!storageKey) return fallback;
    const resourceId = resourceIdFromStorageKey(storageKey);
    if (resourceId) return resourceFileUrl(resourceId);
    return resolveMediaUrl(storageKey, fallback);
}

export async function getMediaBlob(storageKey: string) {
    if (resourceIdFromStorageKey(storageKey)) return getCachedResourceBlob(storageKey);
    return store.getItem<Blob>(storageKey);
}

export async function resolveGeneratedVideoUrl(storageKey: string, signal?: AbortSignal) {
    const scope = getActiveUserScope();
    try {
        signal?.throwIfAborted();
        if (resourceIdFromStorageKey(storageKey)) {
            const url = await cacheResourceObjectUrlDurably(storageKey);
            signal?.throwIfAborted();
            return url;
        }
        if (!storageKey) throw new Error("视频结果缺少本地缓存标识");
        const blob = await store.getItem<Blob>(storageKey);
        signal?.throwIfAborted();
        if (scope !== getActiveUserScope()) throw new DOMException("用户已切换", "AbortError");
        if (!(blob instanceof Blob) || !blob.size) throw new Error("视频文件尚未写入本地缓存");
        const url = objectUrls.get(storageKey) || URL.createObjectURL(blob);
        objectUrls.set(storageKey, url);
        return url;
    } catch (error) {
        if (error instanceof Error && error.name === "AbortError") throw error;
        signal?.throwIfAborted();
        throw new Error(`视频已生成，本地缓存失败，请重新加载资源：${error instanceof Error ? error.message : "缓存不可用"}`, { cause: error });
    }
}

export async function resolveGeneratedVideo(video: { storageKey: string; bytes?: number; mimeType?: string; width?: number; height?: number; durationMs?: number }, signal?: AbortSignal): Promise<UploadedFile> {
    const scope = getActiveUserScope();
    const url = await resolveGeneratedVideoUrl(video.storageKey, signal);
    const blob = await getMediaBlob(video.storageKey);
    signal?.throwIfAborted();
    if (scope !== getActiveUserScope()) throw new DOMException("用户已切换", "AbortError");
    if (!blob?.size) throw new Error("生成视频本地缓存为空，未标记为成功");
    return { ...video, url, bytes: video.bytes || blob.size, mimeType: video.mimeType || blob.type || "video/mp4" };
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

function readAudioMeta(url: string) {
    return new Promise<{ durationMs?: number }>((resolve) => {
        const audio = document.createElement("audio");
        const done = () => resolve({ durationMs: Number.isFinite(audio.duration) ? Math.round(audio.duration * 1000) : undefined });
        audio.onloadedmetadata = done;
        audio.onerror = done;
        audio.src = url;
    });
}
