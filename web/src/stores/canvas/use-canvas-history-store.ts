import { create } from "zustand";
import { persist, type PersistStorage } from "zustand/middleware";

import { localForageStorageForScope } from "@/lib/localforage-storage";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";

export type DeletedCanvasHistoryItem = {
    id: string;
    title: string;
    createdAt: string;
    updatedAt: string;
    deletedAt: string;
    nodeCount: number;
};

type CanvasHistoryStore = {
    deletedProjects: DeletedCanvasHistoryItem[];
    recordDeletedProjects: (projects: CanvasProject[]) => void;
    removeDeletedHistoryItem: (id: string) => void;
    clearDeletedHistory: () => void;
};

export const CANVAS_HISTORY_STORE_KEY = "infinite-canvas:deleted_history_store";

const historyStorage: PersistStorage<CanvasHistoryStore> = {
    getItem: async (name) => {
        const value = await localForageStorageForScope().getItem(name);
        return value ? JSON.parse(value) : null;
    },
    setItem: (name, value) => localForageStorageForScope().setItem(name, JSON.stringify(value)),
    removeItem: (name) => localForageStorageForScope().removeItem(name),
};

export const useCanvasHistoryStore = create<CanvasHistoryStore>()(
    persist(
        (set) => ({
            deletedProjects: [],
            recordDeletedProjects: (projects) => {
                const now = new Date().toISOString();
                const additions = projects.map((project) => ({
                    id: project.id,
                    title: project.title || "未命名画布",
                    createdAt: project.createdAt || project.updatedAt || now,
                    updatedAt: project.updatedAt || now,
                    deletedAt: now,
                    nodeCount: project.nodes?.length || 0,
                }));
                set((state) => {
                    const replacedIds = new Set(additions.map((item) => item.id));
                    return { deletedProjects: [...additions, ...state.deletedProjects.filter((item) => !replacedIds.has(item.id))].slice(0, 200) };
                });
            },
            removeDeletedHistoryItem: (id) => set((state) => ({ deletedProjects: state.deletedProjects.filter((item) => item.id !== id) })),
            clearDeletedHistory: () => set({ deletedProjects: [] }),
        }),
        {
            name: CANVAS_HISTORY_STORE_KEY,
            storage: historyStorage,
        },
    ),
);
