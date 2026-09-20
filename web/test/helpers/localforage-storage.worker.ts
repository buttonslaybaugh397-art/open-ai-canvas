import assert from "node:assert/strict";
import { mock } from "bun:test";

let writes = 0;
mock.module("localforage", () => ({
    default: {
        config: () => undefined,
        getItem: async () => null,
        setItem: async () => {
            writes++;
            throw new Error("No available storage method found");
        },
        removeItem: async () => {
            writes++;
            throw new Error("No available storage method found");
        },
    },
}));
const { localForageStorageForScope } = await import("../../src/lib/localforage-storage");
const storage = localForageStorageForScope("storage-test");
await storage.setItem("state", "server-render");
assert.equal(writes, 0);
Object.defineProperty(globalThis, "window", { value: {}, configurable: true });
assert.equal(await storage.getItem("state"), null);
await assert.rejects(async () => storage.setItem("state", "must-not-be-lost"), /No available storage method/);
await assert.rejects(async () => storage.removeItem("state"), /No available storage method/);
assert.equal(writes, 2);
