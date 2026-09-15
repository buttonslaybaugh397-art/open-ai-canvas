import { afterEach, beforeEach, expect, spyOn, test } from "bun:test";
import localforage from "localforage";
import { ApiError, apiClient } from "../src/services/api/request";
import { CanvasResourceRecovery, remapResourceReferences, subscribeResourceRepairs } from "../src/services/canvas-resource-recovery";
import * as cache from "../src/services/resource-blob-cache";
import { initializeRemoteUserDataSession, resetRemoteUserDataSync, saveRemoteAssetNow, saveRemoteUserDataNow } from "../src/services/user-data-sync";
import { ASSET_STORE_KEY, flushAssetStorePersistence, useAssetStore, type ImageAsset } from "../src/stores/use-asset-store";
import { CANVAS_STORE_KEY, flushCanvasStorePersistence, useCanvasStore, type CanvasProject } from "../src/stores/canvas/use-canvas-store";
import { CanvasNodeType } from "../src/types/canvas";

const originalAdapter = apiClient.defaults.adapter;
const originalWindow = globalThis.window;
const originalAssets = useAssetStore.getState().assets;
const originalProjects = useCanvasStore.getState().projects;
const inline = "data:image/png;base64,aGVsbG8=";
const oldKey = "resource:missing";
const newKey = "resource:restored";
const oldUrl = "/api/resources/missing/file";
const newUrl = "/api/resources/restored/file";
const durable = new Map<string, unknown>();
let cachedBlob: ReturnType<typeof spyOn>;
let primeCache: ReturnType<typeof spyOn>;
let readStorage: ReturnType<typeof spyOn>;
let writeStorage: ReturnType<typeof spyOn>;
let status: string | number;
let requests: string[];
let bodies: { asset?: ImageAsset; project?: CanvasProject }[];
let failRepair = false;

function image(): ImageAsset {
    return {
        id: "asset",
        kind: "image",
        title: "Image",
        tags: [],
        coverUrl: inline,
        data: { storageKey: oldKey, dataUrl: oldUrl, width: 320, height: 180, bytes: 5, mimeType: "image/png" },
        metadata: { resourceKey: oldKey },
        createdAt: "2026-09-10T00:00:00.000Z",
        updatedAt: "2026-09-10T00:00:00.000Z",
    };
}

function canvas(): CanvasProject {
    return {
        id: "canvas",
        title: "Canvas",
        createdAt: "2026-09-10T00:00:00.000Z",
        updatedAt: "2026-09-10T00:00:00.000Z",
        nodes: [
            {
                id: "node",
                type: CanvasNodeType.Image,
                title: "Image",
                width: 320,
                height: 180,
                position: { x: 0, y: 0 },
                metadata: { assetId: "asset", storageKey: oldKey, content: oldUrl },
            },
        ],
        connections: [],
        chatSessions: [],
        activeChatId: null,
        directorScenes: [],
        backgroundMode: "dots",
        showImageInfo: false,
        viewport: { x: 0, y: 0, k: 1 },
        timeline: {
            version: 2,
            tracks: [],
            durationMs: 1000,
            clips: [
                {
                    id: "clip",
                    kind: "image",
                    nodeId: "asset:asset",
                    trackId: "track",
                    startMs: 0,
                    durationMs: 1000,
                    directMedia: { id: "direct", assetId: "asset", kind: "image", title: "Image", storageKey: oldKey, url: oldUrl },
                },
            ],
        },
    };
}

beforeEach(async () => {
    durable.clear();
    const local = new Map([["infinite-canvas:active-user-scope", "recovery-test"]]);
    Object.defineProperty(globalThis, "window", {
        configurable: true,
        value: {
            setTimeout: () => 1,
            clearTimeout: () => undefined,
            localStorage: { getItem: (key: string) => local.get(key) ?? null, setItem: (key: string, value: string) => local.set(key, value) },
        },
    });
    readStorage = spyOn(localforage, "getItem").mockImplementation(async (key: string) => durable.get(key) ?? null);
    writeStorage = spyOn(localforage, "setItem").mockImplementation(async (key: string, value: unknown) => {
        durable.set(key, value);
        return value;
    });
    cachedBlob = spyOn(cache, "getCachedResourceBlob").mockResolvedValue(null);
    primeCache = spyOn(cache, "primeResourceBlobCache").mockResolvedValue("");
    resetRemoteUserDataSync();
    useCanvasStore.setState({ projects: [] });
    useAssetStore.setState({ assets: [] });
    await Promise.all([flushAssetStorePersistence(), flushCanvasStorePersistence()]);
    requests = [];
    bodies = [];
    status = 404;
    failRepair = false;
    apiClient.defaults.adapter = async (config) => {
        requests.push(`${config.method} ${config.url}`);
        let data: unknown = {};
        if (config.method === "get" && config.url === "/resources/missing") {
            if (typeof status === "number") throw new ApiError("resource probe failed", { status });
            data = { resource: { id: "missing", status } };
        } else if (config.url === "/resources") {
            expect(config.headers.get("X-Idempotency-Key")).toBe(oldKey);
            expect(config.data.get("kind")).toBe("image");
            data = { resource: { id: "restored", status: "ready" } };
        } else if (config.url === "/resources/missing/repair-references") {
            expect(JSON.parse(config.data)).toEqual({ replacementResourceId: "restored" });
            if (failRepair) throw new ApiError("repair rejected", { status: 403 });
            data = { repaired: true };
        } else if (config.url === "/assets/asset" && config.method === "get") data = { asset: image() };
        else if (config.url === "/canvas-projects/canvas" && config.method === "get") data = { project: canvas() };
        else if (config.method === "put") bodies.push(JSON.parse(config.data));
        else throw new Error(`Unexpected request: ${config.method} ${config.url}`);
        return { data: { code: 0, data, msg: "" }, status: 200, statusText: "OK", headers: {}, config };
    };
});

afterEach(async () => {
    resetRemoteUserDataSync();
    useCanvasStore.setState({ projects: originalProjects });
    useAssetStore.setState({ assets: originalAssets });
    await Promise.all([flushAssetStorePersistence(), flushCanvasStorePersistence()]);
    cachedBlob.mockRestore();
    primeCache.mockRestore();
    readStorage.mockRestore();
    writeStorage.mockRestore();
    apiClient.defaults.adapter = originalAdapter;
    if (originalWindow === undefined) delete (globalThis as { window?: unknown }).window;
    else Object.defineProperty(globalThis, "window", { configurable: true, value: originalWindow });
});

test("404 reuploads once, repairs references before asset/canvas saves and flushes durable state", async () => {
    await initializeRemoteUserDataSession("recovery-test");
    useAssetStore.setState({ assets: [image()] });
    useCanvasStore.setState({ projects: [canvas()] });
    let liveNodes = canvas().nodes;
    const unsubscribe = subscribeResourceRepairs((remaps) => {
        liveNodes = remapResourceReferences(liveNodes, remaps);
    });
    try {
        await saveRemoteUserDataNow();
        expect(requests).toEqual(["get /resources/missing", "post /resources", "post /resources/missing/repair-references", "put /assets/asset", "put /canvas-projects/canvas"]);
        expect(bodies[0].asset?.data.storageKey).toBe(newKey);
        expect(bodies[0].asset?.metadata?.resourceKey).toBe(newKey);
        expect(bodies[0].asset?.coverUrl).toBe(newUrl);
        expect(bodies[1].project?.nodes[0].metadata?.storageKey).toBe(newKey);
        expect(bodies[1].project?.timeline?.clips[0].directMedia?.url).toBe(newUrl);
        expect(liveNodes[0].metadata?.storageKey).toBe(newKey);
        expect(useAssetStore.getState().assets).toHaveLength(1);
        const assetState = JSON.parse(String(durable.get(`${ASSET_STORE_KEY}:user:recovery-test`)));
        const canvasState = JSON.parse(String(durable.get(`${CANVAS_STORE_KEY}:user:recovery-test`)));
        expect(assetState.state.assets[0].data.storageKey).toBe(newKey);
        expect(canvasState.state.projects[0].nodes[0].metadata.storageKey).toBe(newKey);
        const count = requests.length;
        await saveRemoteUserDataNow();
        expect(requests).toHaveLength(count);
    } finally {
        unsubscribe();
    }
});

test("a dirty canvas repairs an acknowledged asset without overwriting clean remote entities", async () => {
    useAssetStore.setState({ assets: [image()] });
    useCanvasStore.setState({ projects: [canvas()] });
    await initializeRemoteUserDataSession("recovery-test");
    useCanvasStore.getState().renameProject("canvas", "Edited");
    await saveRemoteUserDataNow();
    expect(requests).toContain("post /resources/missing/repair-references");
    expect(requests).not.toContain("put /assets/asset");
    expect(useAssetStore.getState().assets[0].metadata?.resourceKey).toBe(newKey);
    expect(bodies.at(-1)?.project?.title).toBe("Edited");
});

test.each(["failed", "deleted"])("%s resource restores from local-only Blob cache", async (remoteStatus) => {
    status = remoteStatus;
    cachedBlob.mockResolvedValue(new Blob(["image"], { type: "image/png" }));
    const source = { storageKey: oldKey, content: oldUrl };
    const remaps = new Map<string, string>();
    const recovery = new CanvasResourceRecovery(
        () => source,
        async (from, to) => {
            remaps.set(from, to);
        },
        () => {},
    );
    await recovery.prepare(source);
    expect(cachedBlob).toHaveBeenCalledWith(oldKey, { localOnly: true });
    expect(remaps.get(oldKey)).toBe(newKey);
});

test.each([401, 403, 429, 500, 503, "pending", "unknown", "ready"])("status %s never triggers automatic upload", async (remoteStatus) => {
    status = remoteStatus;
    const source = { storageKey: oldKey, content: inline };
    const recovery = new CanvasResourceRecovery(
        () => source,
        async () => {},
        () => {},
    );
    if (remoteStatus === "ready") await recovery.prepare(source);
    else await expect(recovery.prepare(source)).rejects.toThrow();
    expect(requests).toEqual(["get /resources/missing"]);
    expect(cachedBlob).not.toHaveBeenCalled();
});

test("missing local copy is an explicit failure while other canvases still save", async () => {
    await initializeRemoteUserDataSession("recovery-test");
    useCanvasStore.setState({ projects: [canvas(), { ...canvas(), id: "healthy", nodes: [], timeline: undefined }] });
    await expect(saveRemoteUserDataNow()).rejects.toThrow("本机没有可上传");
    expect(requests).not.toContain("post /resources");
    expect(requests).not.toContain("put /canvas-projects/canvas");
    expect(requests).toContain("put /canvas-projects/healthy");
});

test("repair rejection cannot acknowledge or rewrite a scoped asset", async () => {
    await initializeRemoteUserDataSession("recovery-test");
    useAssetStore.setState({ assets: [image()] });
    failRepair = true;
    await expect(saveRemoteAssetNow("asset")).rejects.toThrow("repair rejected");
    expect(useAssetStore.getState().assets[0].metadata?.resourceKey).toBe(oldKey);
    expect(requests).not.toContain("put /assets/asset");
});

test("remapping preserves unrelated edits, narrative strings, URL suffixes and identity for unchanged branches", () => {
    const untouched = { text: oldUrl, title: oldKey, prompt: `use ${oldUrl}` };
    const source = { metadata: { storageKey: oldKey, content: oldUrl + "?variant=playback#x" }, untouched };
    const result = remapResourceReferences(source, new Map([[oldKey, newKey]]));
    expect(result.metadata).toEqual({ storageKey: newKey, content: newUrl + "?variant=playback#x" });
    expect(result.untouched).toBe(untouched);
    expect(source.metadata.storageKey).toBe(oldKey);
});

test("a session change during resource probing stops upload and repair", async () => {
    let valid = true;
    const source = { storageKey: oldKey, content: inline };
    const recovery = new CanvasResourceRecovery(
        () => source,
        async () => {},
        () => {
            if (!valid) throw new Error("account changed");
        },
    );
    const pending = recovery.prepare(source);
    valid = false;
    await expect(pending).rejects.toThrow("account changed");
    expect(requests).not.toContain("post /resources");
});
