import { getMediaBlob } from "@/services/file-storage";
import { getImageBlob } from "@/services/image-storage";
import { deleteRemoteAsset, deleteRemoteCanvasProject, getRemoteUserDataSnapshot, upsertRemoteAsset, upsertRemoteCanvasProject } from "@/services/api/user-data";
import { resourceFileUrl, resourceIdFromStorageKey, resourceStorageKey, uploadResourceFile } from "@/services/api/resources";
import { getActiveUserScope, scopedLocalStorage } from "@/lib/user-scope";
import { flushCanvasStorePersistence } from "@/stores/canvas/use-canvas-store";
import { flushAssetStorePersistence } from "@/stores/use-asset-store";
import type { Asset } from "@/stores/use-asset-store";
import { useAssetStore } from "@/stores/use-asset-store";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";
import { useCanvasStore } from "@/stores/canvas/use-canvas-store";

let activeRemoteUserId = "";
let applyingRemoteState = false;
let syncTimer: number | null = null;
let syncPromise: Promise<void> | null = null;
let syncQueued = false;
let syncPauseCount = 0;
let syncPauseTail: Promise<void> = Promise.resolve();
let resumeSync: (() => void) | null = null;
let syncResumed: Promise<void> | null = null;
let syncRetryAttempt = 0;
const activeRemoteSyncOperations = new Set<Promise<void>>();
let subscriptionsInstalled = false;
let remoteAssetVersions = new Map<string, string>();
let remoteProjectVersions = new Map<string, string>();
let remoteAssetFingerprints = new Map<string, string>();
let remoteProjectFingerprints = new Map<string, string>();
type PendingEntityChange = {
    updatedAt: string;
};

type PendingRemoteChanges = {
    projects: Record<string, PendingEntityChange>;
    assets: Record<string, PendingEntityChange>;
    deletedProjects: Record<string, string>;
    deletedAssets: Record<string, string>;
};

const REMOTE_SYNC_JOURNAL_KEY = "infinite-canvas:remote-user-data-sync";
let pendingRemoteChanges: PendingRemoteChanges | null = null;
let pendingRemoteChangesScope = "";

const LOCAL_STORAGE_KEY_PATTERN = /^(image|video|audio|file|video-reference|audio-reference):/;

export async function syncRemoteUserData(userId?: string | null) {
    activeRemoteUserId = userId || "";
    if (!activeRemoteUserId) return;
    try {
        await runRemoteUserDataSyncOperation(async () => {
            // 登录只拉一次聚合快照。摘要列表再逐条请求详情会把 N 条数据放大成 2N+2 个请求，
            // 并且会在登录阶段同时触发大量媒体解析，任何一项失败都会污染登录结果。
            const snapshot = await getRemoteUserDataSnapshot();
            // 旧后端或测试适配器可能只返回其中一类数据；读取路径按空列表降级，后续本地数据仍会正常补传。
            const remoteProjects = Array.isArray(snapshot.projects) ? snapshot.projects : [];
            const remoteAssets = Array.isArray(snapshot.assets) ? snapshot.assets : [];
            remoteProjectVersions = versionMap(remoteProjects);
            remoteAssetVersions = versionMap(remoteAssets);
            remoteProjectFingerprints = fingerprintMap(remoteProjects);
            remoteAssetFingerprints = fingerprintMap(remoteAssets);
            const localProjects = useCanvasStore.getState().projects;
            const localAssets = useAssetStore.getState().assets;
            // 网络请求期间用户仍可编辑；必须在快照返回后重新读取日志，不能使用请求开始时的旧状态。
            const pending = readPendingRemoteChanges();
            const mergedProjects = mergeById(localProjects, remoteProjects, pending.projects, pending.deletedProjects);
            // 这里只合并结构化素材数据，不在登录阶段解析图片/视频/音频 URL；媒体由实际使用方按需解析。
            const mergedAssets = mergeById(localAssets, remoteAssets, pending.assets, pending.deletedAssets);
            applyingRemoteState = true;
            try {
                useCanvasStore.getState().replaceProjects(mergedProjects);
                useAssetStore.getState().replaceAssets(mergedAssets);
            } finally {
                applyingRemoteState = false;
            }
        });
        // 首次登录可能带有尚未创建到云端的本地画布；先完成一次 upsert，避免详情页保存/分享先于项目创建。
        await saveRemoteUserDataNow();
        syncRetryAttempt = 0;
        scheduleRemoteUserDataSync();
    } catch (error) {
        console.warn("登录后画布首次同步失败，保留本地项目等待重试", error);
        scheduleRemoteUserDataSync();
        throw error;
    }
}

export function installRemoteUserDataAutoSync() {
    if (subscriptionsInstalled) return;
    subscriptionsInstalled = true;
    useCanvasStore.subscribe((state, previous) => {
        if (state.projects !== previous.projects && !applyingRemoteState) {
            recordPendingEntityChanges(state.projects, previous.projects, "projects");
            scheduleRemoteUserDataSync();
        }
    });
    useAssetStore.subscribe((state, previous) => {
        if (state.assets !== previous.assets && !applyingRemoteState) {
            recordPendingEntityChanges(state.assets, previous.assets, "assets");
            scheduleRemoteUserDataSync();
        }
    });
    if (typeof window !== "undefined" && typeof document !== "undefined" && typeof window.addEventListener === "function") {
        const flush = () => {
            void flushLocalAndRemoteUserData();
        };
        window.addEventListener("online", flush);
        window.addEventListener("pagehide", flush);
        if (typeof document.addEventListener === "function") document.addEventListener("visibilitychange", () => {
            if (document.visibilityState === "hidden") flush();
        });
    }
}

export function resetRemoteUserDataSync() {
    activeRemoteUserId = "";
    remoteAssetVersions.clear();
    remoteProjectVersions.clear();
    remoteAssetFingerprints.clear();
    remoteProjectFingerprints.clear();
    if (syncTimer) {
        window.clearTimeout(syncTimer);
        syncTimer = null;
    }
    syncQueued = false;
    syncRetryAttempt = 0;
    pendingRemoteChanges = null;
    pendingRemoteChangesScope = "";
}

function waitForRemoteUserDataSyncResume() {
    if (!syncPauseCount) return Promise.resolve();
    if (!syncResumed) {
        syncResumed = new Promise<void>((resolve) => {
            resumeSync = resolve;
        });
    }
    return syncResumed;
}

async function runRemoteUserDataSyncOperation<T>(operation: () => Promise<T>): Promise<T> {
    while (syncPauseCount) await waitForRemoteUserDataSyncResume();
    let finish!: () => void;
    const active = new Promise<void>((resolve) => {
        finish = resolve;
    });
    activeRemoteSyncOperations.add(active);
    try {
        return await operation();
    } finally {
        activeRemoteSyncOperations.delete(active);
        finish();
    }
}

export async function withRemoteUserDataSyncPaused<T>(operation: () => Promise<T>): Promise<T> {
    syncPauseCount += 1;
    if (syncTimer) {
        window.clearTimeout(syncTimer);
        syncTimer = null;
        syncQueued = true;
    }
    let releaseTurn!: () => void;
    const previousTurn = syncPauseTail;
    syncPauseTail = new Promise<void>((resolve) => {
        releaseTurn = resolve;
    });
    await previousTurn;
    try {
        if (activeRemoteSyncOperations.size) {
            await Promise.allSettled([...activeRemoteSyncOperations]);
        }
        if (syncPromise) await syncPromise;
        return await operation();
    } finally {
        releaseTurn();
        syncPauseCount -= 1;
        if (!syncPauseCount) {
            const resolve = resumeSync;
            resumeSync = null;
            syncResumed = null;
            resolve?.();
            if (syncQueued) scheduleRemoteUserDataSync();
        }
    }
}

export function scheduleRemoteUserDataSync(delay = 1200) {
    if (!activeRemoteUserId || applyingRemoteState) return;
    if (syncPauseCount) {
        syncQueued = true;
        if (syncTimer) {
            window.clearTimeout(syncTimer);
            syncTimer = null;
        }
        return;
    }
    if (syncPromise) {
        syncQueued = true;
        return;
    }
    if (syncTimer) window.clearTimeout(syncTimer);
    syncTimer = window.setTimeout(() => {
        syncTimer = null;
        void saveRemoteUserDataNow().then(() => {
            syncRetryAttempt = 0;
        }).catch((error) => {
            syncRetryAttempt += 1;
            console.warn("云端自动同步失败，稍后重试", error);
            scheduleRemoteUserDataSync(Math.min(30000, 1000 * 2 ** Math.min(syncRetryAttempt, 5)));
        });
    }, delay);
}

async function flushLocalAndRemoteUserData() {
    if (!activeRemoteUserId) return;
    try {
        await Promise.all([flushCanvasStorePersistence(), flushAssetStorePersistence()]);
        await saveRemoteUserDataNow();
        syncRetryAttempt = 0;
    } catch (error) {
        syncRetryAttempt += 1;
        console.warn("页面离开前云端同步失败，保留待同步记录", error);
        scheduleRemoteUserDataSync(Math.min(30000, 1000 * 2 ** Math.min(syncRetryAttempt, 5)));
    }
}

export async function createCanvasProjectWithRemoteSync(title: string, projectId?: string, initialContent?: Partial<Pick<CanvasProject, "nodes" | "connections">>) {
    const id = useCanvasStore.getState().createProject(title, projectId);
    if (initialContent) useCanvasStore.getState().updateProject(id, initialContent);
    if (!activeRemoteUserId) return { id, syncError: new Error("尚未建立云端同步会话") };
    try {
        await saveRemoteUserDataNow();
        return { id };
    } catch (syncError) {
        scheduleRemoteUserDataSync();
        return { id, syncError };
    }
}

export async function deleteAssetWithRemoteSync(id: string) {
    await runRemoteUserDataSyncOperation(async () => {
        markPendingDeletion("assets", id);
        if (activeRemoteUserId) {
            await deleteRemoteAsset(id);
            remoteAssetVersions.delete(id);
        }
        useAssetStore.getState().removeAsset(id);
        clearPendingDeletion("assets", id);
    });
}

export async function saveRemoteUserDataNow() {
    if (!activeRemoteUserId) return;
    if (syncPauseCount) {
        syncQueued = true;
        await waitForRemoteUserDataSyncResume();
        return saveRemoteUserDataNow();
    }
    if (syncPromise) {
        syncQueued = true;
        return syncPromise;
    }
    syncPromise = drainRemoteUserDataChanges();
    try {
        await syncPromise;
    } finally {
        syncPromise = null;
    }
}

async function drainRemoteUserDataChanges() {
    do {
        syncQueued = false;
        await saveRemoteUserDataBatch();
    } while (syncQueued);
}

async function saveRemoteUserDataBatch() {
    try {
        const currentProjects = useCanvasStore.getState().projects;
        const currentAssets = useAssetStore.getState().assets;
        const pending = readPendingRemoteChanges();
        const dirtyProjects = currentProjects.filter((item) => pending.projects[item.id] || !sameRemoteEntity(remoteProjectVersions, remoteProjectFingerprints, item));
        const dirtyAssets = currentAssets.filter((item) => pending.assets[item.id] || !sameRemoteEntity(remoteAssetVersions, remoteAssetFingerprints, item));
        const currentProjectIds = new Set(currentProjects.map((item) => item.id));
        const currentAssetIds = new Set(currentAssets.map((item) => item.id));
        const deletedProjectIds = Object.keys(pending.deletedProjects).filter((id) => !currentProjectIds.has(id) && remoteProjectVersions.has(id));
        const deletedAssetIds = Object.keys(pending.deletedAssets).filter((id) => !currentAssetIds.has(id) && remoteAssetVersions.has(id));
        if (!dirtyProjects.length && !dirtyAssets.length && !deletedProjectIds.length && !deletedAssetIds.length) return;
        markPendingEntities(dirtyProjects, "projects");
        markPendingEntities(dirtyAssets, "assets");
        deletedProjectIds.forEach((id) => markPendingDeletion("projects", id));
        deletedAssetIds.forEach((id) => markPendingDeletion("assets", id));
        const uploaded = new Map<string, string>();
        const projects = await prepareRemoteCanvasProjects(dirtyProjects, uploaded);
        const assets = await prepareRemoteAssets(dirtyAssets, uploaded);
        // SQLite 和接口频控都要求写入保持有界；逐项提交还能准确记录已完成版本。
        for (const project of projects) {
            await upsertRemoteCanvasProject(project);
            remoteProjectVersions.set(project.id, project.updatedAt);
            remoteProjectFingerprints.set(project.id, fingerprint(project));
            replaceLocalEntityAfterRemoteSave("projects", project, dirtyProjects.find((item) => item.id === project.id));
        }
        for (const asset of assets) {
            await upsertRemoteAsset(asset);
            remoteAssetVersions.set(asset.id, asset.updatedAt);
            remoteAssetFingerprints.set(asset.id, fingerprint(asset));
            replaceLocalEntityAfterRemoteSave("assets", asset, dirtyAssets.find((item) => item.id === asset.id));
        }
        for (const id of deletedProjectIds) {
            await deleteRemoteCanvasProject(id);
            remoteProjectVersions.delete(id);
            clearPendingDeletion("projects", id);
        }
        for (const id of deletedAssetIds) {
            await deleteRemoteAsset(id);
            remoteAssetVersions.delete(id);
            clearPendingDeletion("assets", id);
        }
    } finally {
        applyingRemoteState = false;
    }
}

async function prepareRemoteAssets(assets: Asset[], uploaded: Map<string, string>) {
    const result: Asset[] = [];
    for (const asset of assets) result.push(await ensureRemoteResourceReferences(asset, uploaded));
    return result;
}

async function prepareRemoteCanvasProjects(projects: CanvasProject[], uploaded: Map<string, string>) {
    const result: CanvasProject[] = [];
    for (const project of projects) result.push(await ensureRemoteResourceReferences(project, uploaded));
    return result;
}

export async function ensureRemoteResourceReferences<T>(value: T, uploaded = new Map<string, string>()): Promise<T> {
    if (!value || typeof value !== "object") return value;
    if (Array.isArray(value)) {
        const result: unknown[] = [];
        for (const item of value) result.push(await ensureRemoteResourceReferences(item, uploaded));
        return result as T;
    }

    const next: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(value)) {
        next[key] = await ensureRemoteResourceReferences(child, uploaded);
    }

    const storageKey = typeof next.storageKey === "string" ? next.storageKey : "";
    const remoteResourceId = resourceIdFromStorageKey(storageKey);
    if (remoteResourceId) return applyResourceReference(next, storageKey) as T;

    if (!isLocalStorageKey(storageKey)) {
        const inline = inlineMediaDataUrl(next);
        if (!inline) return next as T;
        const resourceStorage = await uploadInlineDataUrl(inline).catch(() => "");
        return (resourceStorage ? applyResourceReference(next, resourceStorage) : next) as T;
    }

    const cached = uploaded.get(storageKey);
    const resourceStorage = cached || (await uploadLocalStorageKey(storageKey, next).catch(() => ""));
    if (!resourceStorage) return next as T;
    uploaded.set(storageKey, resourceStorage);
    return applyResourceReference(next, resourceStorage) as T;
}

function applyResourceReference(payload: Record<string, unknown>, storageKey: string) {
    const url = resourceFileUrl(storageKey.slice("resource:".length));
    payload.storageKey = storageKey;
    for (const key of ["content", "dataUrl", "url", "coverUrl"]) {
        if (typeof payload[key] === "string") payload[key] = url;
    }
    return payload;
}

function inlineMediaDataUrl(payload: Record<string, unknown>) {
    for (const key of ["dataUrl", "content", "url", "coverUrl"]) {
        const value = payload[key];
        if (typeof value === "string" && /^data:(image|video|audio)\//i.test(value)) return value;
    }
    return "";
}

async function uploadInlineDataUrl(dataUrl: string) {
    const blob = await (await fetch(dataUrl)).blob();
    const kind: "image" | "video" | "audio" | "file" = blob.type.startsWith("image/") ? "image" : blob.type.startsWith("video/") ? "video" : blob.type.startsWith("audio/") ? "audio" : "file";
    const resource = await uploadResourceFile(blob, kind);
    return resourceStorageKey(resource.id);
}

async function uploadLocalStorageKey(storageKey: string, payload: Record<string, unknown>) {
    const blob = storageKey.startsWith("image:") ? await getImageBlob(storageKey) : await getMediaBlob(storageKey);
    if (!blob) return "";
    const kind = blob.type.startsWith("image/") ? "image" : blob.type.startsWith("video/") ? "video" : blob.type.startsWith("audio/") ? "audio" : "file";
    const resource = await uploadResourceFile(blob, kind, {
        width: numberValue(payload.naturalWidth) || numberValue(payload.width),
        height: numberValue(payload.naturalHeight) || numberValue(payload.height),
        durationMs: numberValue(payload.durationMs),
    });
    return resourceStorageKey(resource.id);
}

function mergeById<T extends { id?: string; updatedAt?: string }>(
    local: T[],
    remote: T[],
    pending: Record<string, PendingEntityChange>,
    deleted: Record<string, string>,
) {
    const items = new Map<string, T>();
    remote.forEach((item) => {
        if (item.id && !deleted[item.id]) items.set(item.id, item);
    });
    local.forEach((item) => {
        if (!item.id || deleted[item.id]) return;
        const current = items.get(item.id);
        if (!current || pending[item.id] || timeValue(item.updatedAt) >= timeValue(current.updatedAt)) items.set(item.id, item);
    });
    return Array.from(items.values()).sort((a, b) => timeValue(b.updatedAt) - timeValue(a.updatedAt));
}

function emptyPendingRemoteChanges(): PendingRemoteChanges {
    return { projects: {}, assets: {}, deletedProjects: {}, deletedAssets: {} };
}

function readPendingRemoteChanges() {
    const scope = getActiveUserScope();
    if (pendingRemoteChanges && pendingRemoteChangesScope === scope) return pendingRemoteChanges;
    const fallback = emptyPendingRemoteChanges();
    let raw: string | null = null;
    try {
        raw = scopedLocalStorage.getItem(REMOTE_SYNC_JOURNAL_KEY);
    } catch {
        raw = null;
    }
    if (!raw) {
        pendingRemoteChanges = fallback;
        pendingRemoteChangesScope = scope;
        return fallback;
    }
    try {
        const parsed = JSON.parse(raw) as Partial<PendingRemoteChanges>;
        pendingRemoteChanges = {
            projects: normalizePendingEntities(parsed.projects),
            assets: normalizePendingEntities(parsed.assets),
            deletedProjects: isRecord(parsed.deletedProjects) ? parsed.deletedProjects as Record<string, string> : {},
            deletedAssets: isRecord(parsed.deletedAssets) ? parsed.deletedAssets as Record<string, string> : {},
        };
    } catch {
        pendingRemoteChanges = fallback;
    }
    pendingRemoteChangesScope = scope;
    return pendingRemoteChanges;
}

function persistPendingRemoteChanges() {
    if (!activeRemoteUserId) return;
    const pending = readPendingRemoteChanges();
    const hasChanges = Object.keys(pending.projects).length || Object.keys(pending.assets).length || Object.keys(pending.deletedProjects).length || Object.keys(pending.deletedAssets).length;
    try {
        if (hasChanges) scopedLocalStorage.setItem(REMOTE_SYNC_JOURNAL_KEY, JSON.stringify(pending));
        else scopedLocalStorage.removeItem(REMOTE_SYNC_JOURNAL_KEY);
    } catch {
        // 云端同步仍会继续；浏览器不支持 localStorage 时由内存状态和下次自动同步兜底。
    }
}

function recordPendingEntityChanges<T extends { id?: string; updatedAt?: string }>(current: T[], previous: T[], kind: "projects" | "assets") {
    if (!activeRemoteUserId) return;
    const pending = readPendingRemoteChanges();
    const currentById = new Map(current.filter((item): item is T & { id: string } => Boolean(item.id)).map((item) => [item.id, item]));
    const previousById = new Map(previous.filter((item): item is T & { id: string } => Boolean(item.id)).map((item) => [item.id, item]));
    const changes = pending[kind];
    const deleted = kind === "projects" ? pending.deletedProjects : pending.deletedAssets;
    currentById.forEach((item, id) => {
        const previousItem = previousById.get(id);
        if (previousItem && previousItem.updatedAt === item.updatedAt) return;
        changes[id] = { updatedAt: item.updatedAt || "" };
        delete deleted[id];
    });
    previousById.forEach((_item, id) => {
        if (!currentById.has(id)) {
            deleted[id] = new Date().toISOString();
            delete changes[id];
        }
    });
    persistPendingRemoteChanges();
}

function markPendingEntities<T extends { id?: string; updatedAt?: string }>(items: T[], kind: "projects" | "assets") {
    if (!activeRemoteUserId) return;
    const pending = readPendingRemoteChanges();
    const changes = pending[kind];
    const deleted = kind === "projects" ? pending.deletedProjects : pending.deletedAssets;
    for (const item of items) {
        if (!item.id) continue;
        changes[item.id] = { updatedAt: item.updatedAt || "" };
        delete deleted[item.id];
    }
    persistPendingRemoteChanges();
}

function markPendingDeletion(kind: "projects" | "assets", id: string) {
    if (!activeRemoteUserId) return;
    const pending = readPendingRemoteChanges();
    const deleted = kind === "projects" ? pending.deletedProjects : pending.deletedAssets;
    const changes = kind === "projects" ? pending.projects : pending.assets;
    deleted[id] = new Date().toISOString();
    delete changes[id];
    persistPendingRemoteChanges();
}

function clearPendingDeletion(kind: "projects" | "assets", id: string) {
    const pending = readPendingRemoteChanges();
    const deleted = kind === "projects" ? pending.deletedProjects : pending.deletedAssets;
    delete deleted[id];
    persistPendingRemoteChanges();
}

function replaceLocalEntityAfterRemoteSave(kind: "projects" | "assets", saved: CanvasProject | Asset, original?: CanvasProject | Asset) {
    if (!original?.id) return;
    const current = kind === "projects" ? useCanvasStore.getState().projects.find((item) => item.id === saved.id) : useAssetStore.getState().assets.find((item) => item.id === saved.id);
    if (current && fingerprint(current) === fingerprint(original)) {
        applyingRemoteState = true;
        try {
            if (kind === "projects") useCanvasStore.getState().replaceProjects(replaceById(useCanvasStore.getState().projects, [saved as CanvasProject]));
            else useAssetStore.getState().replaceAssets(replaceById(useAssetStore.getState().assets, [saved as Asset]));
        } finally {
            applyingRemoteState = false;
        }
    }
    const pending = readPendingRemoteChanges();
    const changes = pending[kind];
    const marker = changes[original.id];
    if (marker && marker.updatedAt === (original.updatedAt || "")) {
        delete changes[original.id];
        persistPendingRemoteChanges();
    }
}

function fingerprint(value: unknown) {
    return JSON.stringify(value);
}

function fingerprintMap<T extends { id: string }>(items: T[]) {
    return new Map(items.map((item) => [item.id, fingerprint(item)]));
}

function sameRemoteEntity<T extends { id: string; updatedAt?: string }>(versions: Map<string, string>, fingerprints: Map<string, string>, item: T) {
    return Boolean(versions.get(item.id) && versions.get(item.id) === (item.updatedAt || "") && fingerprints.get(item.id) === fingerprint(item));
}

function normalizePendingEntities(value: unknown): Record<string, PendingEntityChange> {
    if (!isRecord(value)) return {};
    return Object.fromEntries(Object.entries(value).flatMap(([id, item]) => {
        if (!isRecord(item) || typeof item.updatedAt !== "string") return [];
        return [[id, { updatedAt: item.updatedAt }]];
    }));
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function versionMap(items: Array<{ id: string; updatedAt?: string }>) {
    return new Map<string, string>(items.map((item) => [item.id, item.updatedAt || ""]));
}

function missingIds<T extends { id: string }>(remote: Map<string, string>, local: T[]) {
    const localIds = new Set(local.map((item) => item.id));
    return Array.from(remote.keys()).filter((id) => !localIds.has(id));
}

function replaceById<T extends { id: string }>(current: T[], changed: T[]) {
    const changedById = new Map(changed.map((item) => [item.id, item]));
    return current.map((item) => changedById.get(item.id) || item);
}

function timeValue(value?: string) {
    const time = value ? Date.parse(value) : 0;
    return Number.isFinite(time) ? time : 0;
}

function sameVersion(remote?: string, local?: string) {
    return Boolean(remote && local) && timeValue(remote) === timeValue(local);
}

function isLocalStorageKey(value: string) {
    return LOCAL_STORAGE_KEY_PATTERN.test(value) && !resourceIdFromStorageKey(value);
}

function numberValue(value: unknown) {
    const number = Number(value);
    return Number.isFinite(number) && number > 0 ? number : undefined;
}
