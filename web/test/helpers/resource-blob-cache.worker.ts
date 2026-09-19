import assert from "node:assert/strict";
import { mock } from "bun:test";

let scope = "user-a";
const stores = new Map<string, Map<string, unknown>>();
let writeGate: Promise<void> | undefined;
let writeStarted: (() => void) | undefined;
let failWrites = false;
mock.module("localforage", () => ({ default: {
    config: () => undefined,
    getItem: async () => null,
    setItem: async (_key: string, value: unknown) => value,
    removeItem: async () => undefined,
    createInstance: ({ storeName }: { storeName: string }) => {
    const values = new Map<string, unknown>();
    stores.set(storeName, values);
    return {
        getItem: async (key: string) => values.get(key) ?? null,
        setItem: async (key: string, value: unknown) => {
            if (storeName === "resource_blobs") {
                writeStarted?.();
                await writeGate;
                if (failWrites) throw new Error("quota exhausted");
            }
            values.set(key, value);
            return value;
        },
        removeItem: async (key: string) => { values.delete(key); },
        length: async () => values.size,
        iterate: async (visit: (value: unknown) => void) => { values.forEach(visit); },
    };
} } }));
const userScope = await import("../../src/lib/user-scope");
mock.module("../../src/lib/user-scope", () => ({ ...userScope, getActiveUserScope: () => scope }));
const { http, ApiError } = await import("../../src/services/api/request");
const cache = await import("../../src/services/resource-blob-cache");
let lookups: string[] = [];
let reads = 0;
let failOnce = false;
let switchOnRead = false;
let denied = false;
http.get = (async (path: string) => {
    lookups.push(path);
    if (denied) throw new ApiError("Forbidden", { status: 403 });
    return { url: `https://cdn.example/${lookups.length}` };
}) as typeof http.get;
globalThis.fetch = (async (_url, options) => {
    assert.equal(options?.credentials, "omit");
    assert.equal(options?.redirect, "error");
    reads++;
    if (failOnce) { failOnce = false; return new Response("retry", { status: 503 }); }
    if (switchOnRead) { switchOnRead = false; scope = "user-b"; }
    return new Response("media", { headers: { "Content-Type": "video/mp4" } });
}) as typeof fetch;

const urls = await Promise.all(Array.from({ length: 12 }, () => cache.cacheResourceObjectUrl("resource:one")));
assert.equal(new Set(urls).size, 1);
assert.equal(reads, 1);
assert.equal(await cache.cacheResourceObjectUrl("resource:one"), urls[0]);
assert.equal(reads, 1);

stores.get("resource_blobs")!.set("user-a:persisted:file", new Blob(["offline"]));
assert.ok((await cache.cacheResourceObjectUrl("resource:persisted")).startsWith("blob:"));
assert.equal(reads, 1);

failOnce = true;
await cache.cacheResourceObjectUrl("resource:retry");
assert.equal(reads, 3);
assert.equal(lookups.filter(path => path === "/resources/retry/file").length, 2);

const adminUrl = await cache.cacheResourceObjectUrl("resource:one", "admin");
assert.notEqual(adminUrl, urls[0]);
assert.equal(lookups.at(-1), "/admin/resources/one/file");

scope = "user-b";
assert.equal(cache.peekCachedResourceObjectUrl("resource:one"), "");
assert.notEqual(await cache.cacheResourceObjectUrl("resource:one"), urls[0]);

scope = "user-a";
switchOnRead = true;
await assert.rejects(cache.getCachedResourceBlob("resource:switch"), { name: "AbortError" });
assert.equal(stores.get("resource_blobs")!.has("user-a:switch:file"), false);
assert.equal(cache.peekCachedResourceObjectUrl("resource:switch"), "");

denied = true;
lookups = [];
const before = reads;
await assert.rejects(cache.cacheResourceObjectUrl("resource:denied"), { status: 403 });
assert.equal(lookups.length, 1);
assert.equal(reads, before);

denied = false;
scope = "user-a";
let releaseWrite!: () => void;
writeGate = new Promise<void>((resolve) => { releaseWrite = resolve; });
const started = new Promise<void>((resolve) => { writeStarted = resolve; });
let ready = false;
const pendingVideo = cache.cacheResourceObjectUrlDurably("resource:durable").then((url) => { ready = true; return url; });
await started;
assert.equal(ready, false, "success must wait for IndexedDB commit");
assert.equal(stores.get("resource_blobs")!.has("user-a:durable:file"), false);
releaseWrite();
assert.ok((await pendingVideo).startsWith("blob:"));
assert.equal(stores.get("resource_blobs")!.has("user-a:durable:file"), true);
writeGate = undefined;
writeStarted = undefined;

const originalWarn = console.warn;
writeGate = new Promise<void>((resolve) => { releaseWrite = resolve; });
const primeStarted = new Promise<void>((resolve) => { writeStarted = resolve; });
await cache.primeResourceBlobCache("resource:primed", new Blob(["video"], { type: "video/mp4" }));
await primeStarted;
let primeReady = false;
const primePending = cache.cacheResourceObjectUrlDurably("resource:primed").then(() => { primeReady = true; });
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(primeReady, false, "an existing objectURL must still wait for the queued write");
releaseWrite();
await primePending;
assert.equal(stores.get("resource_blobs")!.has("user-a:primed:file"), true);
writeGate = undefined;
writeStarted = undefined;
const warnings: unknown[][] = [];
console.warn = (...args) => { warnings.push(args); };
failWrites = true;
await assert.rejects(cache.cacheResourceObjectUrlDurably("resource:quota"), /quota exhausted/);
assert.equal(stores.get("resource_blobs")!.has("user-a:quota:file"), false);
assert.ok(warnings.length > 0);
const readsBeforeRetry = reads;
failWrites = false;
await cache.cacheResourceObjectUrlDurably("resource:quota");
assert.equal(reads, readsBeforeRetry, "retry should persist the existing Blob without downloading again");
assert.equal(stores.get("resource_blobs")!.has("user-a:quota:file"), true);
console.warn = originalWarn;

writeGate = new Promise<void>((resolve) => { releaseWrite = resolve; });
const switchStarted = new Promise<void>((resolve) => { writeStarted = resolve; });
const switched = cache.cacheResourceObjectUrlDurably("resource:commit-switch");
const rejection = assert.rejects(switched, { name: "AbortError" });
await switchStarted;
scope = "user-b";
releaseWrite();
await rejection;
assert.equal(stores.get("resource_blobs")!.has("user-b:commit-switch:file"), false);
writeGate = undefined;
writeStarted = undefined;

const { buildGenerationTaskNodeResult } = await import("../../src/lib/canvas/canvas-generation-task-sync");
const { CanvasNodeType } = await import("../../src/types/canvas");
const node = { id: "video-node", type: CanvasNodeType.Video, title: "video", position: { x: 0, y: 0 }, width: 320, height: 180, metadata: { status: "loading" as const, taskId: "video-task", taskStatus: "succeeded" as const } };
const task = { id: "video-task", type: "canvas_video", status: "succeeded", prompt: "video", attempts: 1, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(), resultJson: JSON.stringify({ video: { storageKey: "resource:node-result", dataUrl: "/api/resources/node-result/file", mimeType: "video/mp4" } }) } as const;
writeGate = new Promise<void>((resolve) => { releaseWrite = resolve; });
const nodeStarted = new Promise<void>((resolve) => { writeStarted = resolve; });
let nodeReady = false;
const nodePending = buildGenerationTaskNodeResult(node, task).then((result) => { nodeReady = true; return result; });
await nodeStarted;
assert.equal(nodeReady, false);
assert.equal(node.metadata.status, "loading");
releaseWrite();
const completed = await nodePending;
assert.equal(completed.metadata?.status, "success");
assert.ok(completed.metadata?.content?.startsWith("blob:"));
assert.equal(stores.get("resource_blobs")!.has("user-b:node-result:file"), true);
writeGate = undefined;
writeStarted = undefined;

console.warn = (...args) => { warnings.push(args); };
failWrites = true;
await assert.rejects(buildGenerationTaskNodeResult(node, { ...task, resultJson: JSON.stringify({ video: { storageKey: "resource:node-failure", dataUrl: "/api/resources/node-failure/file" } }) }), /本地缓存失败/);
assert.equal(node.metadata.status, "loading");
failWrites = false;
console.warn = originalWarn;

const { getResourceOSSUrl } = await import("../../src/services/api/resources");
let finishUrl!: (value: { url: string }) => void;
let urlLookups = 0;
http.get = (() => { urlLookups++; return new Promise((resolve) => { finishUrl = resolve; }); }) as typeof http.get;
scope = "user-a";
const oldUrl = getResourceOSSUrl("resource:oss-scope");
const oldUrlRejected = assert.rejects(oldUrl, /账号已切换/);
scope = "user-b";
finishUrl({ url: "https://cdn.example/account-a" });
await oldUrlRejected;
const newUrl = getResourceOSSUrl("resource:oss-scope");
finishUrl({ url: "https://cdn.example/account-b" });
assert.equal(await newUrl, "https://cdn.example/account-b");
assert.equal(await getResourceOSSUrl("resource:oss-scope"), "https://cdn.example/account-b");
assert.equal(urlLookups, 2);
console.log(JSON.stringify({ checks: 14 }));
