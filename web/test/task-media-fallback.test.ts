import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { parseTaskMediaSources, taskPreviewSource } from "../src/lib/task-media";

function source(path: string) {
    return readFileSync(resolve(import.meta.dir, path), "utf8");
}

describe("media fallback", () => {
    test("keeps the API video URL paired with its resource storage key", () => {
        const sources = parseTaskMediaSources(JSON.stringify({
            mode: "video",
            video: { dataUrl: "/api/resources/resource-1/file", storageKey: "resource:resource-1", mimeType: "video/mp4" },
        }));
        expect(sources).toEqual([{ url: "/api/resources/resource-1/file", storageKey: "resource:resource-1", kind: "video" }]);
        expect(taskPreviewSource({ previewUrl: "/api/resources/resource-1/file", previewKind: "video", previewStorageKey: "resource:resource-1" })).toEqual({ url: "/api/resources/resource-1/file", storageKey: "resource:resource-1", kind: "video" });
    });

    test("does not resolve OSS/CDN during normal media source parsing", () => {
        const preview = source("../src/components/media-preview.tsx");
        expect(preview).toContain("getResourceOSSUrl(fallbackStorageKey)");
        expect(preview.indexOf("getResourceOSSUrl(fallbackStorageKey)")).toBeGreaterThan(preview.indexOf('if (kind === "video" && activeSrc === src'));
    });

    test("restores cached canvas media from storage keys without blocking on persistence", () => {
        const nodeContent = source("../src/components/canvas/canvas-node-content.tsx");
        const cachedImage = source("../src/components/cached-resource-image.tsx");
        const cache = source("../src/services/resource-blob-cache.ts");

        expect(nodeContent).toContain("Boolean(props.node.metadata?.content || props.node.metadata?.storageKey)");
        expect(nodeContent).toContain("!node.metadata?.content && !node.metadata?.storageKey");
        expect(nodeContent).toContain("resourceFileUrl(resourceId)");
        expect(cachedImage).toContain('!src.startsWith("blob:")');
        expect(cachedImage).toContain("resourceFileUrl(resourceId)");
        expect(cache).toContain("void enqueuePersist(target, blob)");
        expect(cache).not.toContain("await enqueuePersist(target, blob)");
    });

    test("replaces failed image and video elements with an unavailable state", () => {
        const preview = source("../src/components/media-preview.tsx");

        expect(preview).toContain("setUnavailable(true)");
        expect(preview).toContain("activeSrc === src");
        expect(preview).toContain("onError={handleUnavailable}");
        expect(preview).toContain("预览不可用，素材可能已删除");
        expect(preview).toContain("<ImageOff");
    });

    test("uses the fallback in list, grid, detail and enlarged previews", () => {
        const list = source("../src/pages/tasks/task-list-row.tsx");
        const grid = source("../src/pages/tasks/task-grid-card.tsx");
        const page = source("../src/pages/tasks/index.tsx");

        expect(list).toContain("<MediaPreview");
        expect(list).toContain("disabled={previewUnavailable}");
        expect(grid).toContain("<MediaPreview");
        expect(page.match(/<MediaPreview/g)).toHaveLength(2);
    });

    test("uses the fallback in admin log thumbnails and enlarged previews", () => {
        const page = source("../src/pages/admin/logs/logs-page.tsx");

        expect(page.match(/<MediaPreview/g)).toHaveLength(2);
        expect(page).toContain("disabled={previewUnavailable}");
        expect(page).toContain("onUnavailable={() => setUnavailableUrl(url)}");
    });
});

describe("task cancellation policy", () => {
    test("does not expose cancellation after a task is created", () => {
        const list = source("../src/pages/tasks/task-list-row.tsx");
        const grid = source("../src/pages/tasks/task-grid-card.tsx");
        const page = source("../src/pages/tasks/index.tsx");

        expect(list).not.toContain("isTaskCancellable");
        expect(list).not.toContain("取消任务");
        expect(grid).not.toContain("isTaskCancellable");
        expect(grid).not.toContain("取消任务");
        expect(page).not.toContain("cancelGenerationTask");
        expect(page).not.toContain('runAction(detailTask.id, "cancel")');
        expect(page).toContain('if (task.status === "queued" || task.status === "running")');
        expect(page).toContain("任务正在执行，不能删除本机记录");
    });

    test("batch stop only applies to items still waiting locally", () => {
        const batches = source("../src/pages/canvas/use-canvas-generation-batches.ts");

        expect(batches).toContain('item.status === "waiting" && !nodeById.get(item.nodeId)?.metadata?.taskId');
        expect(batches).toContain('item.status === "waiting" && stoppableItems.some((candidate) => candidate.id === item.id)');
        expect(batches).not.toContain('item.status === "waiting" || item.status === "submitting"');
    });
});
