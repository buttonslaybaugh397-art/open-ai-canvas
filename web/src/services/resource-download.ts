import { saveAs } from "file-saver";

import { getResourceDirectDownloadUrl, isResourceUrl, resourceIdFromStorageKey, resourceProxyDownloadUrl } from "@/services/api/resources";
import { getStoredResourceBlob } from "@/services/resource-blob-cache";

type DownloadMediaFileOptions = {
    url?: string;
    storageKey?: string;
    fileName: string;
};

export async function downloadMediaFile({ url, storageKey, fileName }: DownloadMediaFileOptions) {
    const resourceId = resourceIdFromStorageKey(storageKey);
    const resourceURL = Boolean(resourceId && url && isResourceUrl(url));
    const safeFileName = safeDownloadFileName(fileName);
    let lastError: unknown = null;

    // Browser navigation can consume a cross-origin media response without CORS.
    // Fetching it as a Blob cannot, so never retry cross-origin URLs through fetch.
    if (url && !resourceURL) {
        triggerNativeDownload(url, safeFileName);
        return;
    }

    if (resourceId) {
        try {
            triggerNativeDownload(await getResourceDirectDownloadUrl(storageKey || "", safeFileName), safeFileName);
            return;
        } catch (error) {
            lastError = error;
        }
        try {
            triggerNativeDownload(resourceProxyDownloadUrl(resourceId, safeFileName), safeFileName);
            return;
        } catch (error) {
            lastError = error;
        }
    }

    if (storageKey) {
        const cached = await getStoredResourceBlob(storageKey).catch(() => null);
        if (cached) {
            saveAs(cached, safeFileName);
            return;
        }
    }

    // Preserve the browser's native cross-origin download attempt for
    // external URLs that are not backed by a server resource.
    if (url && !resourceId) {
        triggerNativeDownload(url, safeFileName);
        return;
    }

    throw lastError instanceof Error ? lastError : new Error("媒体下载失败，请稍后重试");
}

function triggerNativeDownload(url: string, fileName: string) {
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = fileName;
    anchor.rel = "noopener noreferrer";
    anchor.style.display = "none";
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
}

function safeDownloadFileName(value: string) {
    const normalized = value
        .replace(/[\\/:*?"<>|]/g, "_")
        .replace(/[. ]+$/g, "")
        .trim();
    return normalized || "download";
}
