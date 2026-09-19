import assert from "node:assert/strict";
import { mock } from "bun:test";

let scope = "user-a";
const stores = new Map<string, Map<string, unknown>>();
mock.module("localforage", () => ({ default: { createInstance: ({ storeName }: { storeName: string }) => {
    const values = new Map<string, unknown>();
    stores.set(storeName, values);
    return {
        getItem: async (key: string) => values.get(key) ?? null,
        setItem: async (key: string, value: unknown) => { values.set(key, value); return value; },
        removeItem: async (key: string) => { values.delete(key); },
        length: async () => values.size,
        iterate: async (visit: (value: unknown) => void) => { values.forEach(visit); },
    };
} } }));
mock.module("../../src/lib/user-scope", () => ({ getActiveUserScope: () => scope }));
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
console.log(JSON.stringify({ checks: 7 }));
