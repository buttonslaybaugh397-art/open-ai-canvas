import { saveAs } from "file-saver";

import { getResourceOSSUrl, isResourceUrl, resourceIdFromStorageKey, resourceProxyFileUrl } from "@/services/api/resources";
import { getStoredResourceBlob } from "@/services/resource-blob-cache";

type DownloadMediaFileOptions = {
    url?: string;
    storageKey?: string;
    fileName: string;
};

const DOWNLOAD_ATTEMPTS = 3;

export async function downloadMediaFile({ url, storageKey, fileName }: DownloadMediaFileOptions) {
    const resourceId = resourceIdFromStorageKey(storageKey);
    let lastError: unknown = null;

    // Try the URL already returned by the API first. This keeps successful
    // downloads off the application proxy and preserves provider-side delivery.
    if (url) {
        try {
            saveAs(await fetchBlobWithRetry(url), safeDownloadFileName(fileName));
            return;
        } catch (error) {
            lastError = error;
        }
    }

    if (resourceId) {
        try {
            const directUrl = await getResourceOSSUrl(storageKey || "");
            if (directUrl) {
                try {
                    saveAs(await fetchBlobWithRetry(directUrl), safeDownloadFileName(fileName));
                    return;
                } catch (error) {
                    lastError = error;
                }
            }
        } catch (error) {
            lastError = error;
        }
        try {
            saveAs(await fetchBlobWithRetry(resourceProxyFileUrl(resourceId)), safeDownloadFileName(fileName));
            return;
        } catch (error) {
            lastError = error;
        }
    }

    if (storageKey) {
        const cached = await getStoredResourceBlob(storageKey).catch(() => null);
        if (cached) {
            saveAs(cached, safeDownloadFileName(fileName));
            return;
        }
    }

    // Preserve the browser's native cross-origin download attempt for
    // external URLs that are not backed by a server resource.
    if (url && !resourceId) {
        saveAs(url, safeDownloadFileName(fileName));
        return;
    }

    throw lastError instanceof Error ? lastError : new Error("媒体下载失败，请稍后重试");
}

async function fetchBlobWithRetry(url: string) {
    let lastError: unknown = null;
    for (let attempt = 0; attempt < DOWNLOAD_ATTEMPTS; attempt += 1) {
        try {
            const response = await fetch(url, { credentials: isResourceUrl(url) ? "include" : "same-origin" });
            if (!response.ok) throw new Error(`媒体读取失败（${response.status}）`);
            return await response.blob();
        } catch (error) {
            lastError = error;
        }
        if (attempt < DOWNLOAD_ATTEMPTS - 1) await new Promise((resolve) => window.setTimeout(resolve, 250 * (attempt + 1)));
    }
    throw lastError instanceof Error ? lastError : new Error("媒体读取失败");
}

function safeDownloadFileName(value: string) {
    const normalized = value.replace(/[\\/:*?"<>|]/g, "_").replace(/[. ]+$/g, "").trim();
    return normalized || "download";
}
