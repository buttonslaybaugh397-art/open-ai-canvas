import { cacheResourceObjectUrl } from "@/services/resource-blob-cache";
import type { ResourceBlobAccess } from "@/services/api/resources";

type DownloadErrorHandler = (error: Error) => void;

export async function downloadResourceFile(storageKey: string, fileName: string, onError: DownloadErrorHandler, access: ResourceBlobAccess = "user") {
    try {
        const url = await cacheResourceObjectUrl(storageKey, access);
        if (!url) throw new Error("资源不可用，无法下载");
        saveLocalFile(url, fileName);
    } catch (error) {
        onError(error instanceof Error ? error : new Error("资源下载失败"));
    }
}

// A redirect to another origin can ignore <a download>. Keep that navigation
// sandboxed away from the editor, even when the CDN returns inline media/errors.
export function downloadMediaFile(url: string, fileName: string, onError: DownloadErrorHandler) {
    const target = new URL(url, window.location.href);
    if (!["http:", "https:", "blob:", "data:"].includes(target.protocol)) {
        onError(new Error("不支持的资源下载地址"));
        return;
    }
    if (target.protocol === "blob:" || target.protocol === "data:") {
        saveLocalFile(target.href, fileName);
        return;
    }

    const frame = document.createElement("iframe");
    frame.hidden = true;
    frame.title = "资源下载";
    frame.setAttribute("sandbox", "allow-downloads allow-same-origin");
    frame.referrerPolicy = "no-referrer";
    let fallingBack = false;
    const controller = new AbortController();
    const cleanup = () => {
        window.clearTimeout(cleanupTimer);
        controller.abort();
        frame.remove();
    };
    // Attachment responses are handed to the browser download manager and do
    // not emit load. Leave each frame alive long enough for slow response headers.
    const cleanupTimer = window.setTimeout(cleanup, 10 * 60 * 1000);
    frame.onload = () => {
        try {
            if (frame.contentWindow?.location.href === "about:blank") return;
        } catch {
            // A loaded cross-origin document is inline media, not a download.
        }
        if (fallingBack) return;
        fallingBack = true;
        window.clearTimeout(cleanupTimer);
        void fetch(target.href, {
            credentials: "same-origin",
            referrerPolicy: "no-referrer",
            signal: controller.signal,
        }).then(async (response) => {
            if (!response.ok) throw new Error(`资源下载失败（HTTP ${response.status}）`);
            const contentType = response.headers.get("content-type")?.split(";", 1)[0].trim().toLowerCase();
            if (contentType && ["text/html", "application/json", "application/xml", "text/xml"].includes(contentType)) {
                throw new Error("下载地址未返回媒体文件，请检查资源是否失效或登录是否过期");
            }
            const blob = await response.blob();
            const objectUrl = URL.createObjectURL(blob);
            try {
                saveLocalFile(objectUrl, fileName);
            } finally {
                window.setTimeout(() => URL.revokeObjectURL(objectUrl), 60_000);
            }
        }).catch((error: unknown) => {
            onError(error instanceof TypeError
                ? new Error("资源下载失败，请检查网络及 CDN 的附件下载与 CORS 配置")
                : error instanceof Error ? error : new Error("资源下载失败"));
        }).finally(cleanup);
    };
    frame.src = target.href;
    document.body.appendChild(frame);
}

function saveLocalFile(url: string, fileName: string) {
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = fileName;
    anchor.style.display = "none";
    document.body.appendChild(anchor);
    try {
        anchor.click();
    } finally {
        anchor.remove();
    }
}
