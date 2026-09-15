import { getMediaBlob } from "@/services/file-storage";
import { getImageBlob } from "@/services/image-storage";
import { deleteRemoteAsset, deleteRemoteCanvasProject, getRemoteAsset, getRemoteAssetsByIds, getRemoteCanvasProject, getRemoteUserDataSnapshot, listRemoteAssetsPage, upsertRemoteAsset, upsertRemoteCanvasProject } from "@/services/api/user-data";
import { appQueryClient } from "@/lib/query-client";
import { resourceFileUrl, resourceIdFromStorageKey, resourceStorageKey, uploadResourceFile } from "@/services/api/resources";
import { CanvasResourceRecovery, notifyResourceRepairs, remapResourceReferences } from "@/services/canvas-resource-recovery";
import { parseAssetRecordList } from "@/lib/asset-record";
import { assetForRemoteSync } from "@/lib/asset-remote-sync";
import type { Asset } from "@/stores/use-asset-store";
import { flushAssetStorePersistence, useAssetStore } from "@/stores/use-asset-store";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";
import { flushCanvasStorePersistence, useCanvasStore } from "@/stores/canvas/use-canvas-store";
import { useSyncProgressStore } from "@/stores/use-sync-progress-store";
import { useCanvasHistoryStore } from "@/stores/canvas/use-canvas-history-store";
import { repairMissingCanvasAssets } from "@/services/canvas-asset-repair";
import { canvasNodeToAsset } from "@/lib/canvas/canvas-node-asset";

let activeRemoteUserId = "";
type RemoteUserDataPhase = "inactive" | "hydrating" | "ready" | "failed";

let remoteUserDataPhase: RemoteUserDataPhase = "inactive";
let syncTimer: number | null = null;
let syncPromise: Promise<void> | null = null;
let syncQueued = false;
let remoteOperationTail: Promise<void> = Promise.resolve();
let subscriptionsInstalled = false;
let acknowledgedAssets = new Map<string, Asset>();
let acknowledgedProjects = new Map<string, CanvasProject>();
let incrementalSession = false;
let sessionEpoch = 0;
const verifiedProjects = new Set<string>();
const verifiedAssets = new Set<string>();
const remoteProjectLoadPromises = new Map<string, Promise<CanvasProject | undefined>>();

export async function initializeRemoteUserDataSession(userId: string) {
    await withRemoteUserDataSyncExclusive(async () => {
        resetRemoteUserDataSync();
        activeRemoteUserId = userId;
        incrementalSession = true;
        acknowledgedProjects = new Map(useCanvasStore.getState().projects.map((project) => [project.id, project]));
        acknowledgedAssets = new Map(useAssetStore.getState().assets.map((asset) => [asset.id, asset]));
        remoteUserDataPhase = "ready";
    });
}

export async function loadCanvasProjectForEditing(id: string) {
    const pending = remoteProjectLoadPromises.get(id);
    if (pending) return pending;
    const epoch = sessionEpoch;
    const request = withRemoteUserDataSyncExclusive(async () => {
        if (epoch !== sessionEpoch) throw new Error("账号已切换，请重新打开画布");
        const local = useCanvasStore.getState().projects.find((project) => project.id === id);
        if (!activeRemoteUserId || verifiedProjects.has(id)) return local;
        const { project } = await getRemoteCanvasProject(id);
        await loadReferencedAssets(collectAssetIds(project));
        const current = useCanvasStore.getState().projects.find((candidate) => candidate.id === id);
        if (current && !sameEntitySnapshot(acknowledgedProjects.get(id), current)) {
            // 当前页面已经开始编辑本地缓存时，远端详情只建立新的冲突基线；
            // 保留当前画布内容，后续保存由当前打开的画布显式覆盖远端版本，
            // 避免旧缓存被永久卡在“自动重试但永远冲突”的状态。
            acknowledgedProjects.set(id, project);
            verifiedProjects.add(id);
            return current;
        }
        acknowledgedProjects.set(id, project);
        verifiedProjects.add(id);
        useCanvasStore.setState((state) => ({ projects: [...state.projects.filter((candidate) => candidate.id !== id), project] }));
        return project;
    });
    remoteProjectLoadPromises.set(id, request);
    const clearPending = () => {
        if (remoteProjectLoadPromises.get(id) === request) remoteProjectLoadPromises.delete(id);
    };
    void request.then(clearPending, clearPending);
    return request;
}

async function waitForRemoteProjectLoads() {
    const pending = [...remoteProjectLoadPromises.values()];
    if (pending.length) await Promise.all(pending);
}

export async function loadAssetLibraryPage(options: Parameters<typeof listRemoteAssetsPage>[0]) {
    const epoch = sessionEpoch;
    const result = await listRemoteAssetsPage(options);
    await withRemoteUserDataSyncExclusive(async () => {
        if (epoch !== sessionEpoch) throw new Error("账号已切换，请重新读取素材");
        acceptRemoteAssets(result.assets);
    });
    return { ...result, assets: parseAssetRecordList(result.assets) };
}

function acceptRemoteAssets(remoteAssets: Asset[]) {
    const assets = parseAssetRecordList(remoteAssets);
    const current = new Map(useAssetStore.getState().assets.map((asset) => [asset.id, asset]));
    for (const asset of assets) {
        const local = current.get(asset.id);
        if (local && !sameEntitySnapshot(acknowledgedAssets.get(asset.id), local)) continue;
        acknowledgedAssets.set(asset.id, asset);
        verifiedAssets.add(asset.id);
        current.set(asset.id, asset);
    }
    useAssetStore.setState({ assets: [...current.values()] });
}

function collectAssetIds(value: unknown, ids = new Set<string>()): Set<string> {
    if (!value || typeof value !== "object") return ids;
    for (const [key, child] of Object.entries(value)) {
        if (key === "assetId" && typeof child === "string" && child) ids.add(child);
        else if (child && typeof child === "object") collectAssetIds(child, ids);
    }
    return ids;
}

async function loadReferencedAssets(ids: Iterable<string>) {
    const pending = [...new Set(ids)].filter((id) => !verifiedAssets.has(id));
    for (let offset = 0; offset < pending.length; offset += 100) {
        const { assets } = await getRemoteAssetsByIds(pending.slice(offset, offset + 100));
        acceptRemoteAssets(assets);
    }
}

export async function loadAssetsForUse(ids: Iterable<string>) {
    const epoch = sessionEpoch;
    const requestedIds = [...new Set(ids)];
    await withRemoteUserDataSyncExclusive(async () => {
        if (epoch !== sessionEpoch) throw new Error("账号已切换，请重新读取素材");
        if (activeRemoteUserId) await loadReferencedAssets(requestedIds);
        const available = new Set(useAssetStore.getState().assets.map((asset) => asset.id));
        if (requestedIds.some((id) => !available.has(id) || (activeRemoteUserId && !verifiedAssets.has(id)))) throw new Error("部分素材不存在或无权访问，请重新选择素材");
    });
}

const LOCAL_STORAGE_KEY_PATTERN = /^(image|video|audio|file|video-reference|audio-reference):/;

export async function syncRemoteUserData(userId?: string | null) {
	// 登录/切换账号时，服务端快照建立新的远端基线；本地 IndexedDB 只负责首屏缓存，
	// 不能把服务端已经删除或当前用户无权访问的实体重新补回去。后续增量保存必须基于这份基线做冲突校验。
	let repairedCanvasAssets = false;
    await withRemoteUserDataSyncExclusive(async () => {
        incrementalSession = false;
        activeRemoteUserId = userId || "";
        acknowledgedProjects.clear();
        acknowledgedAssets.clear();
        if (!activeRemoteUserId) {
            remoteUserDataPhase = "inactive";
            return;
        }
        remoteUserDataPhase = "hydrating";
        try {
            // 登录只拉一次聚合快照。摘要列表再逐条请求详情会把 N 条数据放大成 2N+2 个请求，
            // 并且会在登录阶段同时触发大量媒体解析，任何一项失败都会污染登录结果。
            const snapshot = await getRemoteUserDataSnapshot();
            // 登录时服务端是实体真相。浏览器 IndexedDB 只作为首屏缓存，不能把服务端已删除的记录补回去。
            // 这里只替换结构化记录，不在登录阶段解析图片/视频/音频 URL；媒体由实际使用方按需解析。
            const snapshotAssets = parseAssetRecordList(snapshot.assets);
            useCanvasStore.getState().replaceProjects(snapshot.projects);
            useAssetStore.getState().replaceAssets(snapshotAssets);
            const repair = repairMissingCanvasAssets();
            repairedCanvasAssets = repair.createdAssets > 0 || repair.updatedProjects > 0;
            await Promise.all([flushCanvasStorePersistence(), flushAssetStorePersistence()]);
            acknowledgedProjects = new Map(snapshot.projects.map((project) => [project.id, project]));
            acknowledgedAssets = new Map(snapshotAssets.map((asset) => [asset.id, asset]));
            remoteUserDataPhase = "ready";
        } catch (error) {
            remoteUserDataPhase = "failed";
            throw error;
        }
    });
    if (repairedCanvasAssets) await saveRemoteUserDataNow();
}

export function installRemoteUserDataAutoSync() {
    if (subscriptionsInstalled) return;
    subscriptionsInstalled = true;
    useCanvasStore.subscribe((state, previous) => {
        if (state.projects !== previous.projects) scheduleRemoteUserDataSync();
    });
    useAssetStore.subscribe((state, previous) => {
        if (state.assets !== previous.assets) scheduleRemoteUserDataSync();
    });
}

export function resetRemoteUserDataSync() {
    sessionEpoch += 1;
    incrementalSession = false;
    verifiedProjects.clear();
    verifiedAssets.clear();
    remoteProjectLoadPromises.clear();
    activeRemoteUserId = "";
    remoteUserDataPhase = "inactive";
    acknowledgedAssets.clear();
    acknowledgedProjects.clear();
    if (syncTimer) {
        window.clearTimeout(syncTimer);
        syncTimer = null;
    }
    syncQueued = false;
    useSyncProgressStore.getState().clearAll();
}

export function hasRemoteUserDataSyncSession() {
    return Boolean(activeRemoteUserId) && remoteUserDataPhase === "ready";
}

/**
 * 串行执行用户数据同步、账号切换和登出相关的远端操作。
 *
 * 前一个操作失败只影响它自己，不能让后续操作永远停在 rejected tail；当前操作的
 * 结果仍原样返回，由调用方决定如何提示或重试，避免同步层把写入失败伪装成成功。
 */
export function withRemoteUserDataSyncExclusive<T>(operation: () => Promise<T>): Promise<T> {
    const pending = remoteOperationTail.then(() => undefined, () => undefined).then(operation);
    remoteOperationTail = pending.then(
        () => undefined,
        () => undefined,
    );
    return pending;
}

export function scheduleRemoteUserDataSync() {
    if (!activeRemoteUserId || remoteUserDataPhase !== "ready") return;
    if (syncPromise) {
        syncQueued = true;
        return;
    }
    if (syncTimer) window.clearTimeout(syncTimer);
    syncTimer = window.setTimeout(() => {
        syncTimer = null;
        void saveRemoteUserDataNow().catch((error) => console.warn("云端自动同步失败", error));
    }, 1200);
}

export function formatLocalSavedRemotePending(localAction: string, error: unknown): string {
    const detail = error instanceof Error && error.message.trim() ? error.message.trim() : "未知错误";
    return `${localAction}，云端同步失败：${detail}。将自动重试。`;
}

/** 本地写已成功、云端同步失败：排队同一幂等重试，并返回可直接展示的 warning。不得回滚本地写，也不得说成已保存到云端。 */
export function localSavedRemotePendingMessage(localAction: string, error: unknown): string {
    scheduleRemoteUserDataSync();
    return formatLocalSavedRemotePending(localAction, error);
}

export async function createCanvasProjectWithRemoteSync(title: string, projectId?: string, initialContent?: Partial<Pick<CanvasProject, "nodes" | "connections" | "chatSessions" | "activeChatId">>) {
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
    const epoch = sessionEpoch;
    const assetId = id.trim();
    if (!assetId) throw new Error("素材 ID 不能为空");
    await withRemoteUserDataSyncExclusive(async () => {
        if (epoch !== sessionEpoch) throw new Error("账号已切换，请重新选择要删除的素材");
        if (activeRemoteUserId) {
            requireRemoteUserDataBaseline();
            await deleteRemoteAsset(assetId);
            acknowledgedAssets.delete(assetId);
        }
        await useAssetStore.getState().removeAsset(assetId);
        await flushAssetStorePersistence();
    });
}

export async function deleteCanvasProjectsWithRemoteSync(ids: string[]) {
    const epoch = sessionEpoch;
    const projectIds = [...new Set(ids.map((id) => id.trim()).filter(Boolean))];
    if (!projectIds.length) return;
    if (incrementalSession) {
        for (const id of projectIds) await loadCanvasProjectForEditing(id);
    }
    await withRemoteUserDataSyncExclusive(async () => {
        if (epoch !== sessionEpoch) throw new Error("账号已切换，请重新选择要删除的画布");
        if (activeRemoteUserId) requireRemoteUserDataBaseline();
        const currentProjects = useCanvasStore.getState().projects;
        const projectById = new Map(currentProjects.map((project) => [project.id, project]));
        const deletedProjectIds: string[] = [];
        const deletedProjectObjects: CanvasProject[] = [];
        let deletionError: unknown;
        for (const id of projectIds) {
            try {
                if (activeRemoteUserId) {
                    await deleteRemoteCanvasProject(id);
                    acknowledgedProjects.delete(id);
                }
                useCanvasStore.getState().deleteProjects([id]);
                // 批量删除允许部分成功；每个已成功远端删除的实体都立即落实到本地 durable cache。
                await flushCanvasStorePersistence();
                deletedProjectIds.push(id);
                const project = projectById.get(id);
                if (project) deletedProjectObjects.push(project);
            } catch (error) {
                deletionError = error;
                break;
            }
        }
        if (deletedProjectObjects.length > 0) useCanvasHistoryStore.getState().recordDeletedProjects(deletedProjectObjects);

        if (incrementalSession) {
            void appQueryClient.invalidateQueries({ queryKey: ["canvas-library"] });
            if (deletionError) throw deletionError;
            return;
        }

        // 将属于被删除画布的所有媒体节点安全归档至素材库回收站 (status = "archived")
        const currentAssets = useAssetStore.getState().assets;
        const remainingProjects = useCanvasStore.getState().projects;
        const activeAssetIds = new Set<string>();
        for (const proj of remainingProjects) {
            for (const node of proj.nodes) {
                if (node.metadata?.assetId) activeAssetIds.add(node.metadata.assetId);
            }
            for (const clip of proj.timeline?.clips || []) {
                if (clip.directMedia?.assetId) activeAssetIds.add(clip.directMedia.assetId);
            }
        }
        let assetChanged = false;
        // 1. 已存在的关联素材标记为 archived
        const assetsToArchive = currentAssets.filter((asset) => {
            const canvasId = asset.metadata?.canvasId as string | undefined;
            return canvasId && deletedProjectIds.includes(canvasId) && !activeAssetIds.has(asset.id) && asset.status !== "archived";
        });
        for (const asset of assetsToArchive) {
            useAssetStore.getState().updateAsset(asset.id, { status: "archived" });
            assetChanged = true;
        }

        // 2. 对于画布中尚未入库的媒体节点，直接归档为回收站素材。
        // 素材字段统一交给 canvasNodeToAsset 组装，避免删除路径另起一套尺寸、MIME 和资源定位规则。
        for (const project of deletedProjectObjects) {
            for (const node of project.nodes || []) {
                const isMedia = node.type === "image" || node.type === "video" || node.type === "audio";
                if (!isMedia) continue;

                const existingAsset = node.metadata?.assetId ? currentAssets.find((a) => a.id === node.metadata?.assetId) : undefined;
                if (existingAsset) {
                    const owningCanvasId = existingAsset.metadata?.canvasId as string | undefined;
                    if (owningCanvasId === project.id && !activeAssetIds.has(existingAsset.id) && existingAsset.status !== "archived") {
                        useAssetStore.getState().updateAsset(existingAsset.id, { status: "archived" });
                        assetChanged = true;
                    }
                    continue;
                }

                const archivedAsset = canvasNodeToAsset(node, { canvasId: project.id, source: "canvas-manual" });
                if (!archivedAsset) continue;

                const title = node.title || `${project.title} - ${node.type === "image" ? "图片" : node.type === "video" ? "视频" : "音频"}`;
                const prompt = typeof node.metadata?.prompt === "string" ? node.metadata.prompt : "";
                useAssetStore.getState().addAsset({
                    ...archivedAsset,
                    title,
                    tags: node.type === "audio" ? ["画布音频"] : prompt ? [prompt.slice(0, 16)] : [node.type === "video" ? "画布视频" : "画布生成"],
                    category: "other",
                    status: "archived",
                    source: `已删除画布：${project.title}`,
                    metadata: {
                        ...archivedAsset.metadata,
                        canvasId: project.id,
                        sourceNodeId: node.id,
                    },
                });
                assetChanged = true;
            }
        }

        if (assetChanged) {
            await flushAssetStorePersistence();
            if (activeRemoteUserId) {
                try {
                    await drainRemoteUserDataChanges();
                } catch (syncErr) {
                    scheduleRemoteUserDataSync();
                    console.warn("回收站素材云端同步警告:", syncErr);
                }
            }
        }
        if (deletionError) throw deletionError;
    });
}

/** 只确认本次素材的远端写入，其他画布的历史坏引用不能冒充本次素材同步失败。 */
export async function saveRemoteAssetNow(id: string) {
    const epoch = sessionEpoch;
    const assetId = id.trim();
    if (!assetId) throw new Error("素材 ID 不能为空");
    await withRemoteUserDataSyncExclusive(async () => {
        if (epoch !== sessionEpoch) throw new Error("账号已切换，已停止旧会话保存");
        if (!activeRemoteUserId) throw new Error("尚未建立云端同步会话");
        requireRemoteUserDataBaseline();
        const source = useAssetStore.getState().assets.find((asset) => asset.id === assetId);
        if (!source) throw new Error("素材不存在，请重新选择");
        await verifyRemoteAssetBaseline(source);
        if (sameEntitySnapshot(acknowledgedAssets.get(assetId), source)) return;
        const recovery = createResourceRecovery();
        await recovery.prepare(source);
        await saveRemoteAssetSnapshot(remapResourceReferences(source, recovery.remaps), new Map());
    });
}

export async function saveRemoteUserDataNow() {
    // 这是远端写入的总闸门：只有 phase=ready 且已建立 acknowledged 基线时才能提交。
    // 本地 Zustand/localForage 写成功不等于服务端写成功，任何同步异常都必须继续抛出给调用方。
    const epoch = sessionEpoch;
    if (!activeRemoteUserId) return;
    requireRemoteUserDataBaseline();
    // 多个画布可能正在读取远端详情；写入必须等待校验结果。
    await waitForRemoteProjectLoads();
    if (syncPromise) {
        syncQueued = true;
        return syncPromise;
    }
    syncPromise = withRemoteUserDataSyncExclusive(async () => {
        if (epoch !== sessionEpoch) throw new Error("账号已切换，已停止旧会话保存");
        requireRemoteUserDataBaseline();
        await drainRemoteUserDataChanges();
    });
    try {
        await syncPromise;
    } finally {
        syncPromise = null;
    }
}

async function drainRemoteUserDataChanges() {
    const uploaded = new Map<string, string>();
    do {
        syncQueued = false;
        await saveRemoteUserDataBatch(uploaded);
    } while (syncQueued);
}

async function saveRemoteUserDataBatch(uploaded: Map<string, string>) {
    // 中央兜底：任何调用方只要把持久媒体写进画布，提交前都会先补齐素材记录与 assetId。
    // 页面级入口仍主动入库，以便立即反馈；这里负责阻止遗漏入口形成远端幽灵资源。
    const changedProjectIds = new Set(useCanvasStore.getState().projects.filter((project) => !sameEntitySnapshot(acknowledgedProjects.get(project.id), project)).map((project) => project.id));
    repairMissingCanvasAssets(incrementalSession ? changedProjectIds : undefined, incrementalSession);
    const currentProjects = useCanvasStore.getState().projects;
    const currentAssets = useAssetStore.getState().assets;
    const recovery = createResourceRecovery();
    const dirtyProjects = currentProjects.filter((project) => !sameEntitySnapshot(acknowledgedProjects.get(project.id), project));
    const dirtyAssets = currentAssets.filter((asset) => !sameEntitySnapshot(acknowledgedAssets.get(asset.id), asset));
    if (!dirtyProjects.length && !dirtyAssets.length) return;

    if (incrementalSession) {
        for (const source of dirtyProjects) {
            const baseline = acknowledgedProjects.get(source.id);
            if (!baseline || verifiedProjects.has(source.id)) continue;
            const { project } = await getRemoteCanvasProject(source.id);
            if (Date.parse(project.updatedAt) !== Date.parse(baseline.updatedAt)) {
                // 以远端当前版本作为新的校验基线，继续提交当前打开画布的完整快照。
                // 这是画布编辑态的显式覆盖策略，避免资产同步被旧缓存冲突永久阻塞。
                acknowledgedProjects.set(source.id, project);
            }
            verifiedProjects.add(source.id);
        }
        for (const source of dirtyAssets) {
            await verifyRemoteAssetBaseline(source);
        }
    }

    // 先恢复缺失资源；已保存的素材和画布由服务端原子修复，未保存的实体继续按素材优先提交。
    const assetPreflightErrors = new Map<string, unknown>();
    for (const source of dirtyProjects) {
        useSyncProgressStore.getState().setProjectProgress(source.id, {
            projectId: source.id, total: 0, completed: 0, phase: "uploading", message: "正在检查并修复云端媒体",
        });
    }
    for (const source of dirtyAssets) {
        try {
            await recovery.prepare(source);
        } catch (error) {
            assetPreflightErrors.set(source.id, error);
        }
    }
    const canvasPreflightErrors = new Map<string, unknown>();
    for (const source of dirtyProjects) {
        try {
            await recovery.prepare(source);
        } catch (error) {
            canvasPreflightErrors.set(source.id, error);
        }
    }
    // 只重写捕获快照中的引用，上传期间的新编辑留给下一轮，不能把未提交的编辑标记为已确认。
    const preparedAssets = dirtyAssets.map((source) => remapResourceReferences(source, recovery.remaps));
    const preparedProjects = dirtyProjects.map((source) => remapResourceReferences(source, recovery.remaps));
    for (const source of preparedAssets) {
        if (assetPreflightErrors.has(source.id)) continue;
        try {
            await saveRemoteAssetSnapshot(source, uploaded);
        } catch (error) {
            assetPreflightErrors.set(source.id, error);
        }
    }

    // 素材先于画布提交。这样画布中的 resource: 引用一旦成为远端事实，
    // 对应 Asset 已经存在，刷新或换设备不会出现只占容量、不见素材的窗口。
    const canvasErrors: unknown[] = [...assetPreflightErrors.values()];
    let savedProjects = 0;
    for (const source of preparedProjects) {
        const keysToUpload = collectLocalMediaKeys(source);
        const total = keysToUpload.length;
        if (total > 0) {
            useSyncProgressStore.getState().setProjectProgress(source.id, {
                projectId: source.id,
                total,
                completed: 0,
                phase: "uploading",
                message: "正在同步媒体至云端",
            });
        }
        const onMediaUploaded = () => {
            if (total > 0) {
                useSyncProgressStore.getState().incrementProjectCompleted(source.id);
            }
        };
        try {
            const preflightError = canvasPreflightErrors.get(source.id);
            if (preflightError) throw preflightError;
            const failedAsset = [...collectAssetIds(source)].find((id) => assetPreflightErrors.has(id));
            if (failedAsset) throw assetPreflightErrors.get(failedAsset);
            const remotePayload = await ensureRemoteResourceReferences(source, uploaded, onMediaUploaded);
            if (total > 0) {
                useSyncProgressStore.getState().setProjectProgress(source.id, {
                    phase: "saving",
                    message: "正在保存画布结构",
                });
            }
            await upsertRemoteCanvasProject(sanitizeCanvasProjectForRemoteSync(remotePayload));
            acknowledgedProjects.set(source.id, source);
            verifiedProjects.add(source.id);
            savedProjects += 1;
            useSyncProgressStore.getState().setProjectProgress(source.id, null);
        } catch (error) {
            useSyncProgressStore.getState().setProjectProgress(source.id, {
                projectId: source.id,
                total,
                completed: 0,
                phase: "error",
                message: error instanceof Error ? error.message : "云端同步失败，等待重试",
            });
            canvasErrors.push(error);
        }
    }
    if (savedProjects) void appQueryClient.invalidateQueries({ queryKey: ["canvas-library"] });
    // 失败画布不更新确认基线，也不能阻塞其他画布的独立写入。
    if (canvasErrors.length === 1) throw canvasErrors[0];
    const uniqueErrors = [...new Set(canvasErrors)];
    if (uniqueErrors.length === 1) throw uniqueErrors[0];
    if (uniqueErrors.length > 1) throw new AggregateError(uniqueErrors, `${uniqueErrors.length} 个${assetPreflightErrors.size ? "画布或素材" : "画布"}云端保存失败，请检查同步状态`);
}

function createResourceRecovery() {
    const epoch = sessionEpoch;
    return new CanvasResourceRecovery(
        () => ({ projects: useCanvasStore.getState().projects, assets: useAssetStore.getState().assets }),
        async (from, to) => {
            const remaps = new Map([[from, to]]);
            const state = useCanvasStore.getState();
            const projects = remapResourceReferences(state.projects, remaps);
            if (projects !== state.projects) useCanvasStore.setState({ projects });
            const assets = useAssetStore.getState().assets;
            const repairedAssets = remapResourceReferences(assets, remaps);
            if (repairedAssets !== assets) useAssetStore.setState({ assets: repairedAssets });
            notifyResourceRepairs(remaps);
            await Promise.all([flushCanvasStorePersistence(), flushAssetStorePersistence()]);
            if (epoch !== sessionEpoch) throw new Error("账号已切换，已停止旧会话资源补传");
            // This reference-only change was already committed atomically by the server.
            acknowledgedAssets = new Map([...acknowledgedAssets].map(([id, asset]) => [id, remapResourceReferences(asset, remaps)]));
            acknowledgedProjects = new Map([...acknowledgedProjects].map(([id, project]) => [id, remapResourceReferences(project, remaps)]));
        },
        () => {
            if (epoch !== sessionEpoch) throw new Error("账号已切换，已停止旧会话资源补传");
        },
    );
}

async function verifyRemoteAssetBaseline(source: Asset) {
    const baseline = acknowledgedAssets.get(source.id);
    if (!incrementalSession || !baseline || verifiedAssets.has(source.id)) return;
    const { asset } = await getRemoteAsset(source.id);
    if (Date.parse(asset.updatedAt) !== Date.parse(baseline.updatedAt)) throw new Error("素材远端版本已变化，已停止覆盖，请重新打开素材库");
    verifiedAssets.add(source.id);
}

async function saveRemoteAssetSnapshot(source: Asset, uploaded: Map<string, string>) {
    const remotePayload = await ensureRemoteResourceReferences(assetForRemoteSync(source), uploaded);
    await upsertRemoteAsset(remotePayload);
    acknowledgedAssets.set(source.id, source);
    verifiedAssets.add(source.id);
}

function collectLocalMediaKeys(value: unknown, set = new Set<string>()): string[] {
    if (!value || typeof value !== "object") return [...set];
    if (Array.isArray(value)) {
        for (const item of value) collectLocalMediaKeys(item, set);
        return [...set];
    }
    const record = value as Record<string, unknown>;
    const storageKey = typeof record.storageKey === "string" ? record.storageKey : "";
    if (isLocalStorageKey(storageKey) && !resourceIdFromStorageKey(storageKey)) {
        set.add(storageKey);
    } else {
        const inline = inlineMediaDataUrl(record);
        if (inline) set.add(`${inline.length}:${inline.slice(0, 64)}:${inline.slice(-64)}`);
    }
    for (const child of Object.values(record)) {
        collectLocalMediaKeys(child, set);
    }
    return [...set];
}

async function ensureRemoteResourceReferences<T>(value: T, uploaded = new Map<string, string>(), onUploaded?: () => void): Promise<T> {
    if (!value || typeof value !== "object") return value;
    if (Array.isArray(value)) {
        const result: unknown[] = [];
        for (const item of value) result.push(await ensureRemoteResourceReferences(item, uploaded, onUploaded));
        return result as T;
    }

    const record = value as Record<string, unknown>;
    const data = record.data as Record<string, unknown> | undefined;
    const metadata = record.metadata as Record<string, unknown> | undefined;
    const primaryKey = (typeof data?.storageKey === "string" ? data.storageKey : "") || (typeof metadata?.resourceKey === "string" ? metadata.resourceKey : "");
    const next: Record<string, unknown> = {};
    if (primaryKey && data) next.data = await ensureRemoteResourceReferences({ ...data, storageKey: primaryKey }, uploaded, onUploaded);
    for (const [key, child] of Object.entries(value)) {
        if (key === "data" && primaryKey && data) continue;
        // A primary image cover shares the same resource; do not upload it a second time.
        if (key === "coverUrl" && record.kind === "image" && primaryKey) next[key] = child;
        else next[key] = await ensureRemoteResourceReferences(child, uploaded, onUploaded);
    }
    if (primaryKey && record.kind === "image" && next.data && typeof next.data === "object") {
        const key = (next.data as Record<string, unknown>).storageKey;
        if (typeof key === "string" && resourceIdFromStorageKey(key)) next.coverUrl = resourceFileUrl(resourceIdFromStorageKey(key));
    }

    const storageKey = typeof next.storageKey === "string" ? next.storageKey : typeof next.resourceKey === "string" ? next.resourceKey : "";
    const remoteResourceId = resourceIdFromStorageKey(storageKey);
    if (remoteResourceId) return applyResourceReference(next, storageKey) as T;

    if (!isLocalStorageKey(storageKey)) {
        const inline = inlineMediaDataUrl(next);
        if (!inline) return next as T;
        const identity = await inlineMediaUploadIdentity(inline);
        const cached = uploaded.get(identity);
        if (cached) return applyResourceReference(next, cached) as T;
        const resourceStorage = await uploadInlineDataUrl(inline, identity);
        uploaded.set(identity, resourceStorage);
        onUploaded?.();
        return applyResourceReference(next, resourceStorage) as T;
    }

    const cached = uploaded.get(storageKey);
    if (cached) return applyResourceReference(next, cached) as T;
    const resourceStorage = await uploadLocalStorageKey(storageKey, next);
    uploaded.set(storageKey, resourceStorage);
    onUploaded?.();
    return applyResourceReference(next, resourceStorage) as T;
}

function applyResourceReference(payload: Record<string, unknown>, storageKey: string) {
    const resourceId = resourceIdFromStorageKey(storageKey);
    if (!resourceId) {
        throw new Error(`远端资源引用无效：${storageKey}`);
    }
    const url = resourceFileUrl(resourceId);
    if (typeof payload.resourceKey === "string") payload.resourceKey = storageKey;
    if ("storageKey" in payload || !("resourceKey" in payload)) payload.storageKey = storageKey;
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

async function uploadInlineDataUrl(dataUrl: string, identity: string) {
    const response = await fetch(dataUrl);
    if (!response.ok) throw new Error("内嵌媒体读取失败");
    const blob = await response.blob();
    const kind: "image" | "video" | "audio" | "file" = blob.type.startsWith("image/") ? "image" : blob.type.startsWith("video/") ? "video" : blob.type.startsWith("audio/") ? "audio" : "file";
    const resource = await uploadResourceFile(blob, kind, { idempotencyKey: identity });
    return resourceStorageKey(resource.id);
}

async function inlineMediaUploadIdentity(dataUrl: string) {
    const digest = await globalThis.crypto.subtle.digest("SHA-256", new TextEncoder().encode(dataUrl));
    return `inline:${Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

async function uploadLocalStorageKey(storageKey: string, payload: Record<string, unknown>) {
    const blob = storageKey.startsWith("image:") ? await getImageBlob(storageKey) : await getMediaBlob(storageKey);
    if (!blob) throw new Error(`本地媒体不存在，无法同步：${storageKey}`);
    const kind = blob.type.startsWith("image/") ? "image" : blob.type.startsWith("video/") ? "video" : blob.type.startsWith("audio/") ? "audio" : "file";
    const resource = await uploadResourceFile(blob, kind, {
        width: numberValue(payload.naturalWidth) || numberValue(payload.width),
        height: numberValue(payload.naturalHeight) || numberValue(payload.height),
        durationMs: numberValue(payload.durationMs),
        idempotencyKey: storageKey,
    });
    return resourceStorageKey(resource.id);
}

function requireRemoteUserDataBaseline() {
    if (remoteUserDataPhase !== "ready") throw new Error("云端数据基线尚未建立，已停止写入");
}

function sameEntitySnapshot<T>(acknowledged: T | undefined, current: T) {
    return acknowledged !== undefined && (acknowledged === current || JSON.stringify(acknowledged) === JSON.stringify(current));
}

function isLocalStorageKey(value: string) {
    return LOCAL_STORAGE_KEY_PATTERN.test(value) && !resourceIdFromStorageKey(value);
}

function numberValue(value: unknown) {
    const number = Number(value);
    return Number.isFinite(number) && number > 0 ? number : undefined;
}

function sanitizeCanvasProjectForRemoteSync<T>(project: T): T {
    if (!project || typeof project !== "object") return project;
    const clone = { ...(project as Record<string, unknown>) };
    if (Array.isArray(clone.chatSessions)) {
        clone.chatSessions = clone.chatSessions.map((session) => {
            if (!session || typeof session !== "object") return session;
            const s = { ...(session as Record<string, unknown>) };
            if (Array.isArray(s.messages)) {
                s.messages = s.messages.map((message) => {
                    if (!message || typeof message !== "object" || !message.detail) return message;
                    const m = { ...(message as Record<string, unknown>) };
                    if (m.detail && typeof m.detail === "object") {
                        const d = { ...(m.detail as Record<string, unknown>) };
                        if (Array.isArray(d.results)) {
                            d.results = d.results.map((r) => {
                                if (!r || typeof r !== "object") return r;
                                const res = { ...(r as Record<string, unknown>) };
                                if (res.result && typeof res.result === "object") {
                                    const inner = { ...(res.result as Record<string, unknown>) };
                                    if (inner.data && typeof inner.data === "object") {
                                        const { snapshot: _s, before: _b, after: _a, ...restData } = inner.data as Record<string, unknown>;
                                        inner.data = restData;
                                    }
                                    res.result = inner;
                                }
                                return res;
                            });
                        }
                        m.detail = d;
                    }
                    return m;
                });
            }
            return s;
        });
    }
    return clone as T;
}
