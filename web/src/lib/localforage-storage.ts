import localforage from "localforage";
import type { StateStorage } from "zustand/middleware";

import { scopedStorageKey } from "@/lib/user-scope";

localforage.config({
    name: "infinite-canvas",
    storeName: "app_state",
});

// 无浏览器驱动时读取可为空；浏览器写入仍交给 localforage 报错，不能静默丢弃。
function browserStorageAvailable(): boolean {
    if (typeof window === "undefined") return false;
    return typeof window.localStorage !== "undefined" || typeof window.indexedDB !== "undefined";
}

export function localForageStorageForScope(scope?: string): StateStorage {
    const keyFor = (name: string) => scopedStorageKey(name, scope);
    return {
        getItem: async (name) => {
            if (!browserStorageAvailable()) return null;
            return (await localforage.getItem<string>(keyFor(name))) || null;
        },
        setItem: async (name, value) => {
            if (typeof window === "undefined") return;
            await localforage.setItem(keyFor(name), value);
        },
        removeItem: async (name) => {
            if (typeof window === "undefined") return;
            await localforage.removeItem(keyFor(name));
        },
    };
}

export const localForageStorage: StateStorage = localForageStorageForScope();
