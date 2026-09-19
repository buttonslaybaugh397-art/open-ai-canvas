import { afterEach, beforeEach, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import localforage from "localforage";
import { readCanvasSyncDrafts } from "../src/services/canvas-sync-drafts";
import { flushCanvasStorePersistence } from "../src/stores/canvas/use-canvas-store";
import { flushAssetStorePersistence } from "../src/stores/use-asset-store";
import { apiClient } from "../src/services/api/request";
import { ensureCanvasNodeAsset } from "../src/services/project-asset-sync";
import { initializeRemoteUserDataSession, loadCanvasProjectForEditing, resetRemoteUserDataSync, saveRemoteAssetNow, saveRemoteUserDataNow, withRemoteUserDataSyncExclusive } from "../src/services/user-data-sync";
import { useAssetStore, type Asset } from "../src/stores/use-asset-store";
import { useCanvasStore, withCanvasStorePersistenceSuppressed, type CanvasProject } from "../src/stores/canvas/use-canvas-store";
import { useSyncProgressStore } from "../src/stores/use-sync-progress-store";
import { CanvasNodeType } from "../src/types/canvas";

const previousAdapter = apiClient.defaults.adapter;
const previousProjects = useCanvasStore.getState().projects;
const previousAssets = useAssetStore.getState().assets;
const previousWindow = globalThis.window;
const previousGet = localforage.getItem;
const previousSet = localforage.setItem;
const indexed = new Map<string, unknown>();

function project(id: string): CanvasProject {
    return {
        id,
        revision: 0,
        title: id,
        createdAt: "2026-09-10T00:00:00.000Z",
        updatedAt: "2026-09-10T00:00:00.000Z",
        nodes: [],
        connections: [],
        chatSessions: [],
        activeChatId: null,
        directorScenes: [],
        backgroundMode: "dots",
        showImageInfo: false,
        viewport: { x: 0, y: 0, k: 1 },
    };
}

function asset(id: string): Asset {
    return {
        id,
        kind: "image",
        title: id,
        coverUrl: "",
        tags: [],
        data: { storageKey: "resource:ready-image", dataUrl: "", width: 320, height: 180, bytes: 100, mimeType: "image/png" },
        createdAt: "2026-09-10T00:00:00.000Z",
        updatedAt: "2026-09-10T00:00:00.000Z",
    };
}

beforeEach(() => {
    indexed.clear();
    localforage.getItem = (async (key: string) => indexed.get(key) ?? null) as typeof localforage.getItem;
    localforage.setItem = (async (key: string, value: unknown) => {
        indexed.set(key, value);
        return value;
    }) as typeof localforage.setItem;
    Object.defineProperty(globalThis, "window", {
        configurable: true,
        value: {
            setTimeout: () => 1,
            clearTimeout: () => undefined,
            localStorage: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined },
        },
    });
    resetRemoteUserDataSync();
    withCanvasStorePersistenceSuppressed(() => useCanvasStore.setState({ projects: [] }));
    useAssetStore.setState({ assets: [] });
});

afterEach(async () => {
    resetRemoteUserDataSync();
    await Promise.all([flushCanvasStorePersistence(), flushAssetStorePersistence()]);
    localforage.getItem = previousGet;
    localforage.setItem = previousSet;
    apiClient.defaults.adapter = previousAdapter;
    withCanvasStorePersistenceSuppressed(() => useCanvasStore.setState({ projects: previousProjects }));
    useAssetStore.setState({ assets: previousAssets });
    if (previousWindow === undefined) delete (globalThis as { window?: unknown }).window;
    else Object.defineProperty(globalThis, "window", { configurable: true, value: previousWindow });
});

test("generated asset sync does not submit an unrelated broken canvas or asset", async () => {
    await initializeRemoteUserDataSession("owner");
    const generated = asset("generated");
    useAssetStore.setState({ assets: [generated, asset("unrelated-dirty")] });
    withCanvasStorePersistenceSuppressed(() => useCanvasStore.setState({ projects: [project("broken-canvas")] }));
    const writes: string[] = [];
    apiClient.defaults.adapter = async (config) => {
        writes.push(`${config.method} ${config.url}`);
        if (config.url !== "/assets/generated") throw new Error("unrelated data was submitted");
        return { data: { code: 0, data: {}, msg: "" }, status: 200, statusText: "OK", headers: {}, config };
    };
    const result = await ensureCanvasNodeAsset({
        canvasId: "current-canvas",
        source: "canvas-generation",
        taskId: "task",
        node: {
            id: "node",
            type: CanvasNodeType.Image,
            title: "Result",
            position: { x: 0, y: 0 },
            width: 320,
            height: 180,
            metadata: { assetId: generated.id, taskId: "task", storageKey: "resource:ready-image", content: "/api/resources/ready-image/file" },
        },
    });
    expect(result).toEqual({ assetId: generated.id, created: false, linkedToProject: false });
    await saveRemoteAssetNow(generated.id);
    expect(writes).toEqual(["put /assets/generated"]);
});

test("a failed canvas does not block other canvas writes and is not acknowledged", async () => {
    await initializeRemoteUserDataSession("owner");
    withCanvasStorePersistenceSuppressed(() => useCanvasStore.setState({ projects: [project("broken"), project("healthy")] }));
    const writes: string[] = [];
    let broken = true;
    apiClient.defaults.adapter = async (config) => {
        writes.push(`${config.method} ${config.url}`);
        if (config.url === "/canvas-projects/broken" && broken) throw new Error("missing resource");
        const submitted = JSON.parse(config.data).project as CanvasProject;
        return { data: { code: 0, data: { project: { ...submitted, revision: submitted.revision! + 1 } }, msg: "" }, status: 200, statusText: "OK", headers: {}, config };
    };
    await expect(saveRemoteUserDataNow()).rejects.toThrow("missing resource");
    expect(writes).toEqual(["put /canvas-projects/broken", "put /canvas-projects/healthy"]);
    expect(useSyncProgressStore.getState().syncingProjects.broken?.phase).toBe("error");
    writes.length = 0;
    broken = false;
    await saveRemoteUserDataNow();
    expect(writes).toEqual(["put /canvas-projects/broken"]);
    expect(useSyncProgressStore.getState().syncingProjects.broken?.phase).toBe("done");
});

test("scoped asset save propagates failures and retries only unacknowledged writes", async () => {
    await initializeRemoteUserDataSession("owner");
    useAssetStore.setState({ assets: [asset("pending")] });
    let writes = 0;
    apiClient.defaults.adapter = async (config) => {
        writes += 1;
        if (writes === 1) throw new Error("asset save rejected");
        return { data: { code: 0, data: {}, msg: "" }, status: 200, statusText: "OK", headers: {}, config };
    };
    await expect(saveRemoteAssetNow("pending")).rejects.toThrow("asset save rejected");
    await saveRemoteAssetNow("pending");
    await saveRemoteAssetNow("pending");
    expect(writes).toBe(2);
});

test("cached assets missing from the restored database are not silently recreated", async () => {
    useAssetStore.setState({ assets: [asset("old-asset")] });
    await initializeRemoteUserDataSession("owner");
    const requests: string[] = [];
    apiClient.defaults.adapter = async (config) => {
        requests.push(`${config.method} ${config.url}`);
        throw new Error("asset no longer exists");
    };
    await expect(saveRemoteAssetNow("old-asset")).rejects.toThrow("asset no longer exists");
    expect(requests).toEqual(["get /assets/old-asset"]);
});

test("scoped saves reject missing sessions and an account change while queued", async () => {
    useAssetStore.setState({ assets: [asset("pending")] });
    await expect(saveRemoteAssetNow("pending")).rejects.toThrow("尚未建立云端同步会话");
    await initializeRemoteUserDataSession("owner");
    let release!: () => void;
    const gate = withRemoteUserDataSyncExclusive(
        () =>
            new Promise<void>((resolve) => {
                release = resolve;
            }),
    );
    await new Promise<void>((resolve) => queueMicrotask(resolve));
    await new Promise<void>((resolve) => queueMicrotask(resolve));
    const pending = saveRemoteAssetNow("pending");
    resetRemoteUserDataSync();
    release();
    await gate;
    await expect(pending).rejects.toThrow("账号已切换");
});

test("opening a restored canvas replaces stale cached references before writes start", async () => {
    const cached: CanvasProject = {
        ...project("restored"),
        revision: undefined,
        title: "stale cache",
        nodes: [
            {
                id: "old-node",
                type: CanvasNodeType.Image,
                title: "Old result",
                position: { x: 0, y: 0 },
                width: 320,
                height: 180,
                metadata: { taskId: "old-task", assetId: "missing-asset", storageKey: "resource:missing-before-restore", content: "/api/resources/missing-before-restore/file" },
            },
        ],
    };
    const remote = { ...project("restored"), title: "restored database", updatedAt: "2026-09-11T00:00:00.000Z" };
    withCanvasStorePersistenceSuppressed(() => useCanvasStore.setState({ projects: [cached] }));
    await initializeRemoteUserDataSession("owner");
    const requests: string[] = [];
    apiClient.defaults.adapter = async (config) => {
        requests.push(`${config.method} ${config.url}`);
        return { data: { code: 0, data: { project: remote }, msg: "" }, status: 200, statusText: "OK", headers: {}, config };
    };
    expect((await loadCanvasProjectForEditing("restored"))?.title).toBe("restored database");
    expect(useCanvasStore.getState().projects[0]?.nodes).toEqual([]);
    await saveRemoteUserDataNow();
    expect(requests).toEqual(["get /canvas-projects/restored"]);
    const source = readFileSync(new URL("../src/pages/canvas/use-canvas-project-lifecycle.ts", import.meta.url), "utf8");
    const loadBody = source.slice(source.indexOf("const load = async () =>"), source.indexOf("void load()"));
    expect(loadBody).toContain("onLoad: applyRestoredProject");
    expect(loadBody).not.toContain("applyRestoredProject(cachedProject)");
    expect((await readCanvasSyncDrafts("restored"))[0]?.project.nodes[0]?.id).toBe("old-node");
});

test("multiple canvas failures remain visible while successful canvases are acknowledged", async () => {
    await initializeRemoteUserDataSession("owner");
    withCanvasStorePersistenceSuppressed(() => useCanvasStore.setState({ projects: [project("bad-one"), project("good"), project("bad-two")] }));
    const writes: string[] = [];
    apiClient.defaults.adapter = async (config) => {
        writes.push(String(config.url));
        if (config.url !== "/canvas-projects/good") throw new Error(`save rejected: ${config.url}`);
        const submitted = JSON.parse(config.data).project as CanvasProject;
        return { data: { code: 0, data: { project: { ...submitted, revision: submitted.revision! + 1 } }, msg: "" }, status: 200, statusText: "OK", headers: {}, config };
    };
    await expect(saveRemoteUserDataNow()).rejects.toBeInstanceOf(AggregateError);
    expect(writes).toEqual(["/canvas-projects/bad-one", "/canvas-projects/good", "/canvas-projects/bad-two"]);
    expect(Object.entries(useSyncProgressStore.getState().syncingProjects).filter(([, progress]) => progress.phase === "error").map(([id]) => id).sort()).toEqual(["bad-one", "bad-two"]);
    expect(useSyncProgressStore.getState().syncingProjects.good?.phase).toBe("done");
    writes.length = 0;
    await expect(saveRemoteUserDataNow()).rejects.toThrow("2 个画布云端保存失败");
    expect(writes).toEqual(["/canvas-projects/bad-one", "/canvas-projects/bad-two"]);
});

test("scoped asset verification refuses a changed remote version", async () => {
    const cached = asset("changed");
    useAssetStore.setState({ assets: [cached] });
    await initializeRemoteUserDataSession("owner");
    const requests: string[] = [];
    apiClient.defaults.adapter = async (config) => {
        requests.push(`${config.method} ${config.url}`);
        return { data: { code: 0, data: { asset: { ...cached, updatedAt: "2026-09-11T00:00:00.000Z" } }, msg: "" }, status: 200, statusText: "OK", headers: {}, config };
    };
    await expect(saveRemoteAssetNow("changed")).rejects.toThrow("素材远端版本已变化");
    expect(requests).toEqual(["get /assets/changed"]);
});
