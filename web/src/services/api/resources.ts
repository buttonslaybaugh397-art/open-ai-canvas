import { getActiveUserScope } from "@/lib/user-scope";
import { http, apiBaseURL, ApiError } from "@/services/api/request";
import type { OSSConnectionTestInput, OSSConnectionTestResult, OSSProvider, S3Preset } from "@/lib/oss-settings";

export type RemoteResource = {
    id: string;
    userId: string;
    kind: "image" | "video" | "audio" | "file" | string;
    status: "pending" | "ready" | "failed" | "deleted" | string;
    provider: string;
    endpoint: string;
    bucket: string;
    objectKey: string;
    publicUrl: string;
    mimeType: string;
    size: number;
    width?: number;
    height?: number;
    durationMs?: number;
    etag?: string;
    playbackStatus?: string;
    playbackObjectKey?: string;
    playbackError?: string;
    error?: string;
    createdAt: string;
    updatedAt: string;
};

export type UserOSSSetting = {
    enabled: boolean;
    provider: OSSProvider;
    s3Preset: S3Preset;
    region: string;
    endpoint: string;
    cdnBaseUrl: string;
    bucket: string;
    accessKeyId: string;
    hasAccessKeySecret: boolean;
    sessionToken?: string;
    hasSessionToken: boolean;
    pathStyle: boolean;
    allowUserS3: boolean;
    publicBaseUrl: string;
    pathPrefix: string;
    testedAt?: string;
    testedDigest?: string;
    historyCount?: number;
    referencedResourceCount?: number;
    updatedAt?: string;
};

export type UserOSSSettingInput = Pick<UserOSSSetting, "enabled" | "provider" | "s3Preset" | "region" | "endpoint" | "cdnBaseUrl" | "bucket" | "accessKeyId" | "pathPrefix" | "pathStyle"> & {
    accessKeySecret?: string;
    sessionToken?: string;
};

export type AccountFileStorageUsage = {
    usedBytes: number;
    totalBytes: number;
};

export type ArkPrivateAssetSync = {
    resourceId: string;
    status: "active" | string;
};

export type ResourceUploadMeta = {
    width?: number;
    height?: number;
    durationMs?: number;
    fileName?: string;
    idempotencyKey?: string;
};

/**
 * 资源直传失败。
 *
 * `permanent` 是这条边界上唯一重要的信息：媒体直传失败后，调用方默认会把文件留在本机
 * IndexedDB，并由云端数据同步用同一幂等键重传（见 user-data-sync 的 uploadLocalStorageKey）。
 * 但鉴权失效、越权、请求本身不合法这几类失败重传多少次都是同样结果，把它们也归入
 * "稍后自动同步" 等于向用户撒谎，必须当场抛出。
 */
export class ResourceUploadError extends Error {
    readonly status?: number;
    /** true 表示重试不会自愈，调用方不得降级为本地暂存。 */
    readonly permanent: boolean;

    constructor(message: string, options: { status?: number; permanent: boolean; cause?: unknown }) {
        super(message, options.cause === undefined ? undefined : { cause: options.cause });
        this.name = "ResourceUploadError";
        this.status = options.status;
        this.permanent = options.permanent;
    }
}

const resourceCache = new Map<string, RemoteResource>();
const resourceRequests = new Map<string, Promise<RemoteResource>>();
const missingResourceIds = new Set<string>();
const ossUrlCache = new Map<string, { url: string; expiresAt: number }>();
const ossUrlRequests = new Map<string, Promise<string>>();
const OSS_URL_CACHE_TTL_MS = 4 * 60 * 1000;

export function resourceStorageKey(id: string) {
    return `resource:${id}`;
}

export function getUserOSSSetting() {
    return http.get<{ setting: UserOSSSetting }>("/settings/oss");
}

export function updateUserOSSSetting(input: UserOSSSettingInput) {
    return http.patch<{ setting: UserOSSSetting }>("/settings/oss", input);
}

export function testUserOSSConnection(input: OSSConnectionTestInput) {
    return http.post<OSSConnectionTestResult>("/settings/oss/test", input);
}

export async function getAccountFileStorageUsage() {
    const data = await http.get<{ usage: AccountFileStorageUsage }>("/resources/storage-usage");
    return data.usage;
}

export async function syncResourceToArkPrivateAsset(id: string) {
    const data = await http.post<{ sync: ArkPrivateAssetSync }>(`/resources/${encodeURIComponent(id)}/ark-private-asset`);
    return data.sync;
}

export function resourceIdFromStorageKey(storageKey?: string) {
    return storageKey?.startsWith("resource:") ? storageKey.slice("resource:".length) : "";
}

export function isResourceUrl(url?: string) {
    const base = String(apiBaseURL).replace(/\/+$/, "");
    const path = url?.split(/[?#]/, 1)[0] || "";
    return path.startsWith(`${base}/resources/`) && path.endsWith("/file");
}

// 超过该阈值（与后端单请求 multipart 上限 50MB 一致）的本地媒体走分片上传，避免大视频导入失败。
const CHUNK_UPLOAD_THRESHOLD = 50 << 20;
const CHUNK_UPLOAD_CONCURRENCY = 3;
const CHUNK_UPLOAD_ATTEMPTS = 3;

export async function uploadResourceFile(file: Blob, kind: "image" | "video" | "audio" | "file", meta?: ResourceUploadMeta, onProgress?: (uploadedBytes: number, totalBytes: number) => void): Promise<RemoteResource> {
    const name = meta?.fileName || (file instanceof File ? file.name : `${kind}.${extensionFromMime(file.type, kind)}`);
    // 分片与 multipart 两条路径的失败都要归一成 ResourceUploadError，
    // 否则调用方只能靠文案猜测该重试还是该报错。
    try {
        if (file.size > CHUNK_UPLOAD_THRESHOLD) {
            const resource = await uploadFileInChunks(file, name, kind, meta, onProgress);
            resourceCache.set(resourceCacheKey(resource.id), resource);
            return resource;
        }
        const formData = new FormData();
        formData.append("kind", kind);
        formData.append("file", file, name);
        if (meta?.width) formData.append("width", String(Math.round(meta.width)));
        if (meta?.height) formData.append("height", String(Math.round(meta.height)));
        if (meta?.durationMs) formData.append("durationMs", String(Math.round(meta.durationMs)));
        const data = await http.post<{ resource: RemoteResource }>("/resources", formData, {
            ...uploadRequestConfig(meta?.idempotencyKey),
            onUploadProgress: onProgress
                ? ({ loaded, total }) => {
                      if (total && total > 0) onProgress(Math.min(file.size, (file.size * loaded) / total), file.size);
                  }
                : undefined,
        });
        resourceCache.set(resourceCacheKey(data.resource.id), data.resource);
        return data.resource;
    } catch (error) {
        throw normalizeUploadError(error);
    }
}

// 分片上传限制并发，只重试未确认分片；合并失败不得自动重开会话整传。
async function uploadFileInChunks(file: Blob, name: string, kind: "image" | "video" | "audio" | "file", meta: ResourceUploadMeta | undefined, onProgress?: (uploadedBytes: number, totalBytes: number) => void) {
    const session = await http.post<{ uploadId: string; chunkSize: number; chunkCount: number }>(
        "/resources/uploads",
        { fileName: name, kind, size: file.size, width: meta?.width, height: meta?.height, durationMs: meta?.durationMs },
        uploadRequestConfig(meta?.idempotencyKey),
    );
    if (
        !session ||
        typeof session.uploadId !== "string" ||
        !session.uploadId.trim() ||
        !Number.isSafeInteger(session.chunkSize) ||
        session.chunkSize <= 0 ||
        !Number.isSafeInteger(session.chunkCount) ||
        session.chunkCount !== Math.ceil(file.size / session.chunkSize)
    ) {
        throw new ResourceUploadError("服务端返回的分片信息无效", { permanent: true });
    }
    const uploaded = new Map<number, number>();
    let totalUploaded = 0;
    let nextIndex = 0;
    const controller = new AbortController();
    let failure: unknown;
    const report = (index: number, bytes: number) => {
        if (controller.signal.aborted) return;
        const previous = uploaded.get(index) || 0;
        const next = Math.max(previous, bytes);
        uploaded.set(index, next);
        totalUploaded += next - previous;
        onProgress?.(Math.min(totalUploaded, file.size), file.size);
    };
    const worker = async () => {
        try {
            while (!controller.signal.aborted && nextIndex < session.chunkCount) {
                const index = nextIndex++;
                const start = index * session.chunkSize;
                const blob = file.slice(start, Math.min(file.size, start + session.chunkSize));
                for (let attempt = 0; ; attempt++) {
                    try {
                        await http.put<{ index: number }>(`/resources/uploads/${encodeURIComponent(session.uploadId)}/chunks/${index}`, blob, {
                            headers: { "Content-Type": "application/octet-stream" },
                            signal: controller.signal,
                            onUploadProgress: onProgress ? ({ loaded }) => report(index, Math.min(loaded, blob.size)) : undefined,
                        });
                        report(index, blob.size);
                        break;
                    } catch (error) {
                        if (controller.signal.aborted || attempt >= CHUNK_UPLOAD_ATTEMPTS - 1 || normalizeUploadError(error).permanent) throw error;
                        const delay = error instanceof ApiError && error.retryAfterMs !== undefined ? error.retryAfterMs : 500 * 2 ** attempt;
                        await waitForChunkRetry(delay, controller.signal);
                    }
                }
            }
        } catch (error) {
            if (!controller.signal.aborted) {
                failure = error;
                controller.abort();
            }
        }
    };
    await Promise.all(Array.from({ length: Math.min(CHUNK_UPLOAD_CONCURRENCY, session.chunkCount) }, worker));
    if (controller.signal.aborted) throw failure;
    const complete = await http.post<{ resource: RemoteResource }>(`/resources/uploads/${encodeURIComponent(session.uploadId)}/complete`);
    return complete.resource;
}

function waitForChunkRetry(delay: number, signal: AbortSignal) {
    return new Promise<void>((resolve, reject) => {
        const abort = () => {
            clearTimeout(timer);
            signal.removeEventListener("abort", abort);
            reject(new DOMException("请求已取消", "AbortError"));
        };
        const timer = setTimeout(
            () => {
                signal.removeEventListener("abort", abort);
                resolve();
            },
            Math.max(0, delay),
        );
        signal.addEventListener("abort", abort, { once: true });
        if (signal.aborted) abort();
    });
}

// 失败分类直接复用 request() 已经算好的 ApiError.retryable（408/425/429/5xx 可重试），
// 不在这里另起一套响应码判断，避免两处规则漂移。
// 差别只有一处：没有拿到任何 HTTP 响应（断网、超时）时 retryable 为 false，
// 但这种失败恰恰是最该退回本机等待重传的，因此单独按瞬时处理。
function normalizeUploadError(error: unknown): ResourceUploadError {
    if (error instanceof ResourceUploadError) return error;
    // 请求取消不是上传失败，保持原始语义交给调用方。
    if (error instanceof DOMException && error.name === "AbortError") throw error;
    if (error instanceof ApiError) {
        const status = error.status;
        const permanent = status !== undefined && !error.retryable;
        // 即使误超 50MB multipart 上限（后端 http.MaxBytesError），也给出可读中文而非英文裸错。
        if (status === 400 && /body too large|MaxBytes/i.test(error.message)) {
            return new ResourceUploadError("文件过大，请使用小于 50MB 的文件或稍后重试", { status, permanent: true, cause: error });
        }
        if (status === 413) return new ResourceUploadError("文件过大，无法上传", { status, permanent: true, cause: error });
        return new ResourceUploadError(error.message || "上传失败", { status, permanent, cause: error });
    }
    return new ResourceUploadError(error instanceof Error ? error.message : "上传失败", { permanent: false, cause: error });
}

export async function importResourceFromUrl(url: string, kind: "image" | "video" | "audio" | "file", meta?: Omit<ResourceUploadMeta, "fileName">) {
    const data = await http.post<{ resource: RemoteResource }>("/resources/import", { url, kind, width: meta?.width, height: meta?.height, durationMs: meta?.durationMs }, uploadRequestConfig(meta?.idempotencyKey));
    resourceCache.set(resourceCacheKey(data.resource.id), data.resource);
    return data.resource;
}

function uploadRequestConfig(idempotencyKey?: string) {
    const value = idempotencyKey?.trim();
    return value ? { headers: { "X-Idempotency-Key": value } } : undefined;
}

export function getResource(id: string): Promise<RemoteResource> {
    const cacheKey = resourceCacheKey(id);
    const cached = resourceCache.get(cacheKey);
    if (cached) return Promise.resolve(cached);
    if (missingResourceIds.has(cacheKey)) return Promise.reject(new Error("资源不存在或已被删除"));
    const pending = resourceRequests.get(cacheKey);
    if (pending) return pending;
    const task = http
        .get<{ resource: RemoteResource }>(`/resources/${encodeURIComponent(id)}`)
        .then((data) => {
            resourceCache.set(cacheKey, data.resource);
            return data.resource;
        })
        .catch((error) => {
            if (error instanceof ApiError && error.status === 404) missingResourceIds.add(cacheKey);
            throw error;
        })
        .finally(() => resourceRequests.delete(cacheKey));
    resourceRequests.set(cacheKey, task);
    return task;
}

// refreshResource 绕过缓存强制拉取资源最新状态（转码副本就绪轮询用），并回写缓存。
export function refreshResource(id: string): Promise<RemoteResource> {
    const cacheKey = resourceCacheKey(id);
    return http.get<{ resource: RemoteResource }>(`/resources/${encodeURIComponent(id)}`).then((data) => {
        resourceCache.set(cacheKey, data.resource);
        missingResourceIds.delete(cacheKey);
        return data.resource;
    });
}

export async function getResourceOSSUrl(storageKey?: string) {
    const id = resourceIdFromStorageKey(storageKey);
    if (!id) throw new Error("当前媒体尚未上传到后端资源存储");
    const cacheKey = resourceCacheKey(id);
    const cached = ossUrlCache.get(cacheKey);
    if (cached && cached.expiresAt > Date.now()) return cached.url;
    const pending = ossUrlRequests.get(cacheKey);
    if (pending) return pending;
    const request = (async () => {
        try {
            const data = await http.get<{ url: string }>(`/resources/${encodeURIComponent(id)}/oss-url`);
            if (cacheKey !== resourceCacheKey(id)) throw new Error("账号已切换，请重新读取媒体地址");
            if (!data.url) throw new Error("后端未返回对象存储地址");
            ossUrlCache.set(cacheKey, { url: data.url, expiresAt: Date.now() + OSS_URL_CACHE_TTL_MS });
            return data.url;
        } catch (error) {
            if (error instanceof ApiError) throw new Error(error.message || "获取对象存储地址失败");
            throw error;
        } finally {
            ossUrlRequests.delete(cacheKey);
        }
    })();
    ossUrlRequests.set(cacheKey, request);
    return request;
}

function resourceCacheKey(id: string) {
    return `${getActiveUserScope()}:${id}`;
}

export function resourceFileUrl(id: string) {
    const base = String(apiBaseURL).replace(/\/+$/, "");
    return `${base}/resources/${encodeURIComponent(id)}/file`;
}

// Let the browser receive the object-storage/CDN response directly instead of buffering a large media file in the canvas tab.
export function resourceDownloadUrl(id: string, fileName?: string) {
    const query = new URLSearchParams({ direct: "1", download: "1" });
    if (fileName?.trim()) query.set("filename", fileName.trim());
    return resourceFileUrl(id) + "?" + query.toString();
}

export function resolveResourceUrl(storageKey?: string, fallback = "") {
    const id = resourceIdFromStorageKey(storageKey);
    // 资源引用本身已经包含稳定 ID；恢复/展示阶段不需要再查一遍元数据。
    // 需要 publicUrl、mime 或尺寸时必须显式调用 getResource，避免隐式 N+1。
    return id ? resourceFileUrl(id) : fallback;
}

// playbackVariantUrl 返回浏览器兼容播放副本 URL（H.265→H.264 转码结果）。
// 副本就绪前由后端回退原件，调用方再按需降级。
export function playbackVariantUrl(id: string) {
    const base = String(apiBaseURL).replace(/\/+$/, "");
    return `${base}/resources/${encodeURIComponent(id)}/file?variant=playback&direct=1`;
}

export type ResourceBlobAccess = "user" | "admin";

export async function getResourceBlob(storageKey: string, signal?: AbortSignal, access: ResourceBlobAccess = "user") {
    const id = resourceIdFromStorageKey(storageKey);
    if (!id) return null;
    // Resolve with API credentials, then read CDN bytes without application cookies.
    // An empty URL is reserved for resources actually stored on the local server.
    const endpoint = `${access === "admin" ? "/admin" : ""}/resources/${encodeURIComponent(id)}/file`;
    const delivery = await http.get<{ url: string }>(endpoint, {
        params: { resolve: "1" },
        signal,
    });
    if (!delivery || typeof delivery.url !== "string") throw new Error("后端未返回有效的媒体读取地址");
    let response: Response;
    try {
        response = await fetch(delivery.url || `${String(apiBaseURL).replace(/\/+$/, "")}${endpoint}`, {
            credentials: delivery.url ? "omit" : "include",
            // A local resource can be promoted between resolve and fetch. Retry
            // resolution instead of following a new URL with API credentials.
            redirect: "error",
            referrerPolicy: "no-referrer",
            signal,
        });
    } catch (error) {
        if (signal?.aborted || (error instanceof DOMException && error.name === "AbortError")) throw error;
        throw new Error("媒体读取失败，请检查网络及对象存储/CDN 的 CORS 配置", { cause: error });
    }
    if (!response.ok)
        throw new ApiError(`媒体读取失败（HTTP ${response.status}）`, {
            status: response.status,
            retryable: [403, 404, 410, 408, 429].includes(response.status) || response.status >= 500,
        });
    const contentLength = response.headers.get("Content-Length")?.trim();
    const expectedBytes = contentLength ? Number(contentLength) : undefined;
    const blob = await response.blob();
    if (expectedBytes !== undefined && Number.isSafeInteger(expectedBytes) && expectedBytes >= 0 && blob.size !== expectedBytes) {
        throw new ApiError("媒体响应不完整（收到 " + blob.size + " / " + expectedBytes + " 字节）", {
            status: 502,
            retryable: true,
        });
    }
    return blob;
}

function extensionFromMime(mimeType: string, kind: string) {
    if (mimeType.includes("png")) return "png";
    if (mimeType.includes("jpeg")) return "jpg";
    if (mimeType.includes("webp")) return "webp";
    if (mimeType.includes("gif")) return "gif";
    if (mimeType.includes("mp4")) return "mp4";
    if (mimeType.includes("webm")) return "webm";
    if (mimeType.includes("mpeg")) return "mp3";
    if (mimeType.includes("wav")) return "wav";
    return kind === "image" ? "png" : "bin";
}
