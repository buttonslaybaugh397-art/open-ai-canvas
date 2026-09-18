import { http, apiBaseURL, compactApiParams } from "@/services/api/request";

export type AdminStorageResource = {
    id: string;
    userId: string;
    userName: string;
    kind: "image" | "video" | "audio" | "file" | string;
    status: "pending" | "ready" | "failed" | "deleted" | string;
    provider: string;
    bucket?: string;
    objectKey: string;
    mimeType: string;
    size: number;
    physicalBytes: number;
    width: number;
    height: number;
    durationMs: number;
    fileUrl: string;
    createdAt: string;
    updatedAt: string;
};

export type AdminResourcePage = {
    items: AdminStorageResource[];
    total: number;
    page: number;
    pageSize: number;
};

export type AdminStorageDimensionStat = {
    count: number;
    logicalBytes: number;
    physicalBytes: number;
};

export type AdminStorageStats = {
    resourceCount: number;
    readyCount: number;
    logicalBytes: number;
    physicalBytes: number;
    localBytes: number;
    remoteBytes: number;
    byKind: Array<AdminStorageDimensionStat & { kind: string }>;
    byProvider: Array<AdminStorageDimensionStat & { provider: string }>;
};

export type AdminResourceQuery = {
    keyword?: string;
    kind?: string;
    status?: string;
    provider?: string;
    userId?: string;
    page?: number;
    pageSize?: number;
};

export type AdminResourceReference = {
    kind: string;
    id: string;
    title: string;
};

export type AdminResourceDeleteBlocked = {
    id: string;
    reason: string;
    references: AdminResourceReference[];
};

export type AdminResourceDeleteResult = {
    deleted: string[];
    blocked: AdminResourceDeleteBlocked[];
};

export async function listAdminResources(query: AdminResourceQuery, signal?: AbortSignal) {
    return http.get<AdminResourcePage>("/admin/resources", { params: compactApiParams(query), signal });
}

export async function getAdminStorageStats(signal?: AbortSignal) {
    const result = await http.get<{ stats: AdminStorageStats }>("/admin/storage/stats", { signal });
    return result.stats;
}

export function deleteAdminResources(resourceIds: string[]) {
    return http.post<AdminResourceDeleteResult>("/admin/resources/delete", { resourceIds });
}

export function adminResourceFileUrl(id: string, download = false, fileName?: string) {
    const base = String(apiBaseURL).replace(/\/+$/, "");
    const url = `${base}/admin/resources/${encodeURIComponent(id)}/file`;
    if (!download) return url;
    const query = new URLSearchParams({ direct: "1", download: "1" });
    if (fileName?.trim()) query.set("filename", fileName.trim());
    return `${url}?${query.toString()}`;
}
