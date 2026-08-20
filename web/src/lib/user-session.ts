import { getAuthSession, getFeatureAvailability, type AuthSessionPayload } from "@/services/api/auth";
import { localForageStorage } from "@/lib/localforage-storage";
import { appQueryClient } from "@/lib/query-client";
import { scopedLocalStorage, setActiveUserScope } from "@/lib/user-scope";
import { CANVAS_STORE_KEY, flushCanvasStorePersistence, useCanvasStore } from "@/stores/canvas/use-canvas-store";
import { ASSET_STORE_KEY, flushAssetStorePersistence, useAssetStore } from "@/stores/use-asset-store";
import { CONFIG_STORE_KEY, defaultConfig, normalizeConfigSnapshot, useConfigStore } from "@/stores/use-config-store";
import { useUserStore } from "@/stores/use-user-store";
import { installRemoteUserDataAutoSync, resetRemoteUserDataSync, syncRemoteUserData, withRemoteUserDataSyncPaused } from "@/services/user-data-sync";
import { withGenerationConsumersPaused } from "@/services/generation-consumer-lifecycle";

export async function switchUserStorageScope(userId?: string | null) {
    await withGenerationConsumersPaused(async () => {
        await withRemoteUserDataSyncPaused(async () => {
            await Promise.all([flushCanvasStorePersistence(), flushAssetStorePersistence()]);
            resetRemoteUserDataSync();
            setActiveUserScope(userId);
        });
    });
}

export async function applyUserSession(payload: AuthSessionPayload) {
    const previousUserId = useUserStore.getState().user?.id || "";
    const nextUserId = payload.user?.id || "";
    useUserStore.getState().setHydrated(false);
    try {
        if (previousUserId !== nextUserId) appQueryClient.clear();
        await switchUserStorageScope(payload.user?.id);
        const [persistedCanvas, persistedAssets] = await Promise.all([localForageStorage.getItem(CANVAS_STORE_KEY), localForageStorage.getItem(ASSET_STORE_KEY)]);
        const persistedConfig = scopedLocalStorage.getItem(CONFIG_STORE_KEY);
        useUserStore.getState().setUser(payload.user);
        useUserStore.getState().setRuntimeLimits(payload.runtimeLimits);
        useUserStore.getState().setDrawingEngine(payload.drawingEngine);
        useUserStore.getState().setFeatures(payload.features);
        await Promise.all([useCanvasStore.persist.rehydrate(), useAssetStore.persist.rehydrate(), useConfigStore.persist.rehydrate()]);
        if (!persistedCanvas) useCanvasStore.setState({ projects: [] });
        if (!persistedAssets) useAssetStore.setState({ assets: [] });
        if (!persistedConfig) {
            const initialSystemConfig = {
                ...defaultConfig,
                channels: payload.systemChannels || [],
                imageModels: undefined,
                videoModels: undefined,
                textModels: undefined,
                audioModels: undefined,
            };
            useConfigStore.getState().replaceConfig(normalizeConfigSnapshot({ config: initialSystemConfig }).config);
        } else {
            useConfigStore.getState().mergeSystemChannels(payload.systemChannels || []);
        }
        installRemoteUserDataAutoSync();
        if (payload.user?.id) await syncRemoteUserData(payload.user.id);
        else resetRemoteUserDataSync();
    } finally {
        useUserStore.getState().setHydrated(true);
    }
}

export async function refreshSystemChannels() {
    const payload = await getAuthSession();
    useConfigStore.getState().mergeSystemChannels(payload.systemChannels || []);
}

export async function refreshFeatureAvailability() {
    const payload = await getFeatureAvailability();
    useUserStore.getState().setFeatures(payload.features);
    return payload.features;
}
