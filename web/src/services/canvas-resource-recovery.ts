import { ApiError } from "@/services/api/request";
import { refreshResource, repairResourceReferences, resourceFileUrl, resourceIdFromStorageKey, resourceStorageKey, uploadResourceFile } from "@/services/api/resources";
import { getCachedResourceBlob, primeResourceBlobCache } from "@/services/resource-blob-cache";

const locatorFields = new Set(["storageKey", "resourceKey", "content", "dataUrl", "url", "coverUrl"]);
const localMediaUrl = /^(blob:|data:(image|video|audio)\/)/i;
type MediaRecord = Record<string, unknown>;
const repairListeners = new Set<(remaps: ReadonlyMap<string, string>) => void>();

export function subscribeResourceRepairs(listener: (remaps: ReadonlyMap<string, string>) => void) {
    repairListeners.add(listener);
    return () => {
        repairListeners.delete(listener);
    };
}

export function notifyResourceRepairs(remaps: ReadonlyMap<string, string>) {
    for (const listener of repairListeners) listener(remaps);
}

function resourceKey(value: unknown): string {
    if (typeof value !== "string") return "";
    if (resourceIdFromStorageKey(value)) return value;
    const base = resourceFileUrl("").replace(/\/file$/, "");
    if (!value.startsWith(base)) return "";
    const match = value.slice(base.length).match(/^([^/?#]+)\/file(?:[?#].*)?$/);
    if (!match) return "";
    try {
        return resourceStorageKey(decodeURIComponent(match[1]));
    } catch {
        return "";
    }
}

// Copy-on-write, restricted to media locators: prompts, titles and chat text are never rewritten.
export function remapResourceReferences<T>(value: T, remaps: ReadonlyMap<string, string>): T {
    if (!value || typeof value !== "object" || !remaps.size) return value;
    let changed = false;
    const result = Array.isArray(value) ? [...value] : { ...value };
    for (const [field, child] of Object.entries(value)) {
        let next = child;
        const replacement = locatorFields.has(field) ? remaps.get(resourceKey(child)) : undefined;
        if (replacement && typeof child === "string") {
            const suffix = child.match(/[?#].*$/)?.[0] || "";
            next = child.startsWith("resource:") ? replacement : resourceFileUrl(resourceIdFromStorageKey(replacement)) + suffix;
        } else if (child && typeof child === "object") {
            next = remapResourceReferences(child, remaps);
        }
        if (next !== child) {
            (result as MediaRecord)[field] = next;
            changed = true;
        }
    }
    return changed ? (result as T) : value;
}

function mediaRecords(value: unknown, result = new Map<string, MediaRecord[]>()): Map<string, MediaRecord[]> {
    if (!value || typeof value !== "object") return result;
    const record = value as MediaRecord;
    const keys = new Set(
        Object.entries(record)
            .filter(([field]) => locatorFields.has(field))
            .map(([, child]) => resourceKey(child))
            .filter(Boolean),
    );
    for (const key of keys) result.set(key, [...(result.get(key) || []), record]);
    // An image asset's cover is a local copy of its primary media, even if dataUrl was stripped.
    const data = record.data as MediaRecord | undefined;
    const metadata = record.metadata as MediaRecord | undefined;
    if (data && typeof data === "object") {
        const key = resourceKey(data.storageKey) || resourceKey(metadata?.resourceKey);
        if (key)
            result.set(key, [
                ...(result.get(key) || []),
                {
                    ...data,
                    kind: record.kind,
                    ...(record.kind === "image" && typeof record.coverUrl === "string" && localMediaUrl.test(record.coverUrl) ? { coverUrl: record.coverUrl } : {}),
                },
            ]);
    }
    for (const child of Object.values(record)) mediaRecords(child, result);
    return result;
}

export class CanvasResourceRecovery {
    private readonly checked = new Map<string, Promise<void>>();
    readonly remaps = new Map<string, string>();

    constructor(
        private readonly sources: () => unknown,
        private readonly onRepaired: (from: string, to: string) => Promise<void>,
        private readonly assertSession: () => void,
    ) {}

    async prepare(value: unknown) {
        const wanted = [...mediaRecords(value).keys()];
        // Bounded metadata probes only for entities being saved; never scan the cloud on login.
        let index = 0;
        const errors: unknown[] = [];
        await Promise.all(
            Array.from({ length: Math.min(4, wanted.length) }, async () => {
                while (index < wanted.length) {
                    const key = wanted[index++];
                    try {
                        let pending = this.checked.get(key);
                        if (!pending) {
                            pending = this.ensure(key);
                            this.checked.set(key, pending);
                        }
                        await pending;
                    } catch (error) {
                        errors.push(error);
                    }
                }
            }),
        );
        if (errors.length) throw errors[0];
    }

    private async ensure(key: string) {
        this.assertSession();
        const id = resourceIdFromStorageKey(key);
        try {
            const resource = await refreshResource(id);
            this.assertSession();
            if (!resource || resource.id !== id) throw new Error("云端资源响应与请求不一致");
            if (resource.status === "ready") return;
            if (resource.status !== "failed" && resource.status !== "deleted") throw new Error(`云端资源尚未就绪，请稍后重试：${id}`);
        } catch (error) {
            if (!(error instanceof ApiError) || error.status !== 404) throw error;
        }
        this.assertSession();
        const candidates = mediaRecords(this.sources()).get(key) || [];
        let blob: Blob | null = null;
        let source: MediaRecord = candidates.find((candidate) => candidate.kind || candidate.mimeType) || {};
        for (const candidate of candidates) {
            for (const field of ["dataUrl", "content", "url", "coverUrl"]) {
                const url = candidate[field];
                if (typeof url !== "string" || !localMediaUrl.test(url)) continue;
                try {
                    const response = await fetch(url);
                    if (!response.ok) throw new Error("本地媒体副本读取失败");
                    blob = await response.blob();
                    source = candidate;
                    break;
                } catch (error) {
                    if (error instanceof DOMException && error.name === "AbortError") throw error;
                    console.warn("本地媒体 URL 已失效，尝试其他缓存副本", { resourceId: id });
                }
            }
            if (blob) break;
        }
        if (!blob) blob = await getCachedResourceBlob(key, { localOnly: true });
        this.assertSession();
        if (!blob || !blob.size) throw new Error(`云端资源缺失且本机没有可上传的媒体副本，请重新导入原文件：${id}`);
        const kind = blob.type.startsWith("image/") ? "image" : blob.type.startsWith("video/") ? "video" : blob.type.startsWith("audio/") ? "audio" : source.kind === "image" || source.kind === "video" || source.kind === "audio" ? source.kind : "file";
        const resource = await uploadResourceFile(blob, kind, {
            idempotencyKey: key,
            width: positiveNumber(source.naturalWidth ?? source.width),
            height: positiveNumber(source.naturalHeight ?? source.height),
            durationMs: positiveNumber(source.durationMs),
        });
        this.assertSession();
        if (!resource.id?.trim() || resource.status !== "ready") throw new Error("补传资源尚未就绪，已停止保存");
        const replacement = resourceStorageKey(resource.id);
        await repairResourceReferences(id, resource.id);
        this.assertSession();
        this.remaps.set(key, replacement);
        this.checked.set(replacement, Promise.resolve());
        try {
            await primeResourceBlobCache(replacement, blob);
        } catch (error) {
            console.warn("补传成功，但本地媒体缓存预热失败", { resourceId: resource.id, error });
        }
        this.assertSession();
        await this.onRepaired(key, replacement);
    }
}

function positiveNumber(value: unknown) {
    return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : undefined;
}
