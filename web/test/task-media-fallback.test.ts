import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { getResourceBlob, isResourceKnownMissing } from "../src/services/api/resources";

function source(path: string) {
    return readFileSync(resolve(import.meta.dir, path), "utf8");
}

describe("media fallback", () => {
    test("negative-caches missing resources and retries only transient failures", async () => {
        const missingKey = `resource:missing-${crypto.randomUUID()}`;
        let missingRequests = 0;
        const missingFetch = async () => {
            missingRequests += 1;
            return new Response(null, { status: 404 });
        };
        expect(await getResourceBlob(missingKey, { fetch: missingFetch })).toBeNull();
        expect(await getResourceBlob(missingKey, { fetch: missingFetch })).toBeNull();
        expect(missingRequests).toBe(1);
        expect(isResourceKnownMissing(missingKey)).toBe(true);

        const forbiddenKey = `resource:forbidden-${crypto.randomUUID()}`;
        let forbiddenRequests = 0;
        const forbiddenFetch = async () => {
            forbiddenRequests += 1;
            return new Response(null, { status: 403 });
        };
        expect(await getResourceBlob(forbiddenKey, { fetch: forbiddenFetch })).toBeNull();
        expect(await getResourceBlob(forbiddenKey, { fetch: forbiddenFetch })).toBeNull();
        expect(forbiddenRequests).toBe(2);
        expect(isResourceKnownMissing(forbiddenKey)).toBe(false);

        const transientKey = `resource:transient-${crypto.randomUUID()}`;
        let transientRequests = 0;
        const delays: number[] = [];
        const transientFetch = async () => {
            transientRequests += 1;
            return transientRequests < 3 ? new Response(null, { status: 503 }) : new Response(new Blob(["ok"]), { status: 200 });
        };
        const blob = await getResourceBlob(transientKey, { fetch: transientFetch, sleep: async (delay) => void delays.push(delay) });
        expect(await blob?.text()).toBe("ok");
        expect(transientRequests).toBe(3);
        expect(delays).toEqual([250, 500]);
    });

    test("retries failed media through the authenticated resource cache before showing unavailable", () => {
        const preview = source("../src/components/media-preview.tsx");

        expect(preview).toContain("cacheResourceObjectUrl(mediaStorageKey)");
        expect(preview).toContain("isResourceKnownMissing(mediaStorageKey)");
        expect(preview).toContain("onError={handleUnavailable}");
        expect(preview).toContain("预览不可用，素材可能已删除");
        expect(preview).toContain("<ImageOff");
    });

    test("video player retries through both proxy and cached resource sources", () => {
        const player = source("../src/components/video-player.tsx");

        expect(player).toContain("resourceProxyFileUrl");
        expect(player).toContain("cacheResourceObjectUrl");
        expect(player).toContain("isResourceKnownMissing");
        expect(player).toContain("videoMimeType");
        expect(player).toContain("video/quicktime");
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
