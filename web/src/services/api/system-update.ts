import { apiClient, request } from "./request";

export type UpdatePhase =
    | "idle"
    | "checking"
    | "ready"
    | "no_update"
    | "preflight"
    | "backing_up"
    | "pulling"
    | "draining"
    | "migrating"
    | "switching"
    | "verifying"
    | "succeeded"
    | "rolling_back"
    | "rolled_back"
    | "failed"
    | "manual_intervention";

export type SystemUpdateRelease = {
    version: string;
    name: string;
    body: string;
    url: string;
    publishedAt: string;
    prerelease: boolean;
};

export type SystemUpdateCheck = {
    key: string;
    label: string;
    status: "passed" | "pending" | "failed" | string;
    detail?: string;
    blocking: boolean;
};

export type SystemUpdateBackup = {
    id: string;
    path: string;
    checksum: string;
    size: number;
    createdAt: string;
    version: string;
};

export type SystemUpdateLog = {
    at: string;
    phase: UpdatePhase;
    message: string;
};

export type SystemUpdateOperation = {
    id?: string;
    phase: UpdatePhase;
    fromVersion?: string;
    targetVersion?: string;
    startedAt?: string;
    finishedAt?: string;
    error?: string;
    rollbackError?: string;
    automaticRollback: boolean;
    logs: SystemUpdateLog[];
};

export type MigrationPhase =
    | "idle"
    | "validating"
    | "backing_up"
    | "stopping"
    | "packaging"
    | "uploading"
    | "restoring"
    | "starting"
    | "verifying"
    | "succeeded"
    | "failed"
    | "manual_intervention";

export type SystemMigrationArchive = {
    id: string;
    checksum: string;
    size: number;
    createdAt: string;
    version: string;
    databaseDriver: string;
};

export type SystemMigrationLog = {
    at: string;
    phase: MigrationPhase;
    message: string;
};

export type SystemMigrationOperation = {
    id?: string;
    kind?: "export" | "import" | string;
    phase: MigrationPhase;
    startedAt?: string;
    finishedAt?: string;
    error?: string;
    archive?: SystemMigrationArchive;
    logs: SystemMigrationLog[];
};

export type SystemMigrationStatus = {
    supported: boolean;
    reason?: string;
    maxArchiveSize: number;
    lastExport?: SystemMigrationArchive;
    operation: SystemMigrationOperation;
};

export type SystemUpdateStatus = {
    supported: boolean;
    connected: boolean;
    repository: string;
    deployment: string;
    currentVersion: string;
    latestRelease?: SystemUpdateRelease;
    updateAvailable: boolean;
    checks: SystemUpdateCheck[];
    lastBackup?: SystemUpdateBackup;
    rollbackVersion?: string;
    operation: SystemUpdateOperation;
    migration: SystemMigrationStatus;
};

export function getSystemUpdateStatus(signal?: AbortSignal) {
    return request<SystemUpdateStatus>(apiClient.get("/admin/system-update", { signal }));
}

export function checkSystemUpdate() {
    return request<SystemUpdateStatus>(apiClient.post("/admin/system-update/check"));
}

export function startSystemUpdate(targetVersion: string) {
    return request<SystemUpdateStatus>(apiClient.post("/admin/system-update/start", { targetVersion }));
}

export function rollbackSystemUpdate(reason: string) {
    return request<SystemUpdateStatus>(apiClient.post("/admin/system-update/rollback", { reason }));
}

export function startMigrationExport() {
    return request<SystemUpdateStatus>(apiClient.post("/admin/system-update/migration/export"));
}

export function importMigrationArchive(archive: File, signal?: AbortSignal) {
    return request<SystemUpdateStatus>(apiClient.post("/admin/system-update/migration/import", archive, {
        signal,
        headers: { "Content-Type": "application/zip" },
        maxBodyLength: Infinity,
        maxContentLength: Infinity,
    }));
}

export async function downloadMigrationArchive() {
    try {
        const response = await apiClient.get<Blob>("/admin/system-update/migration/download", {
            responseType: "blob",
            maxContentLength: Infinity,
        });
        const disposition = response.headers["content-disposition"];
        const match = typeof disposition === "string" ? /filename=(?:"([^"]+)"|([^;\s]+))/.exec(disposition) : null;
        return { blob: response.data, filename: match?.[1] || match?.[2] || "open-ai-canvas-migration.zip" };
    } catch (error) {
        const response = error && typeof error === "object" && "response" in error ? (error as { response?: { status?: number; data?: Blob } }).response : undefined;
        if (response?.data instanceof Blob) {
            const text = await response.data.text();
            if (text.trim()) {
                let envelope: { msg?: string; error?: string } | undefined;
                try {
                    envelope = JSON.parse(text) as { msg?: string; error?: string };
                } catch {
                    envelope = undefined;
                }
                if (envelope?.msg || envelope?.error) {
                    throw new Error(envelope.msg || envelope.error || "下载迁移包失败");
                }
            }
        }
        throw error;
    }
}
