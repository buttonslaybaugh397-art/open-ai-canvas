import { describe, expect, test } from "bun:test";

import { applyGeneratedMediaResultMetadata, generationTaskNodeId, mergeGenerationTaskResultNodes, videoMetadata } from "@/lib/canvas/canvas-generation-task-sync";
import type { GenerationTask } from "@/services/api/task-center";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

function task(overrides: Partial<GenerationTask> = {}): GenerationTask {
    return {
        id: "task-1",
        type: "canvas_image",
        status: "succeeded",
        progress: 100,
        prompt: "生成图片",
        projectId: "canvas-1",
        createdAt: "2026-09-16T01:00:00Z",
        updatedAt: "2026-09-16T01:02:00Z",
        ...overrides,
    } as GenerationTask;
}

function node(overrides: Partial<CanvasNodeData> = {}): CanvasNodeData {
    return {
        id: "node-1",
        type: CanvasNodeType.Image,
        title: "生成中",
        position: { x: 10, y: 20 },
        width: 320,
        height: 320,
        metadata: {
            content: "",
            status: "loading",
            taskId: "task-1",
            taskUpdatedAt: "2026-09-16T01:00:00Z",
        },
        ...overrides,
    };
}

describe("canvas generation task sync", () => {
    test("client context node id takes precedence over stale input metadata", () => {
        expect(generationTaskNodeId(task({
            clientContext: { nodeId: "current-node" },
            inputJson: JSON.stringify({ metadata: { nodeId: "stale-node" } }),
        }))).toBe("current-node");
    });

    test("merges generated media without overwriting user title or movement", () => {
        const base = [node()];
        const current = [node({ title: "用户重命名", position: { x: 80, y: 120 } })];
        const applied = [node({
            title: "生成中",
            position: { x: 0, y: 0 },
            width: 512,
            height: 512,
            metadata: {
                ...base[0].metadata,
                content: "/api/resources/result/file",
                storageKey: "resource:result",
                status: "success",
                taskUpdatedAt: "2026-09-16T01:02:00Z",
            },
        })];

        const merged = mergeGenerationTaskResultNodes(current, base, applied, task(), "node-1");

        expect(merged[0].title).toBe("用户重命名");
        expect(merged[0].position).toEqual({ x: 80, y: 120 });
        expect(merged[0].width).toBe(512);
        expect(merged[0].metadata?.content).toBe("/api/resources/result/file");
        expect(merged[0].metadata?.storageKey).toBe("resource:result");
        expect(merged[0].metadata?.status).toBe("success");
    });

    test("does not apply a stale result after the node starts another task", () => {
        const base = [node()];
        const current = [node({ metadata: { ...base[0].metadata, taskId: "task-2", taskUpdatedAt: "2026-09-16T01:03:00Z" } })];
        const applied = [node({ metadata: { ...base[0].metadata, content: "old-result", status: "success" } })];

        expect(mergeGenerationTaskResultNodes(current, base, applied, task(), "node-1")).toBe(current);
    });

    test("does not replace media changed while result resolution was pending", () => {
        const base = [node()];
        const current = [node({ metadata: { ...base[0].metadata, content: "manual-result", storageKey: "resource:manual" } })];
        const applied = [node({ metadata: { ...base[0].metadata, content: "generated-result", storageKey: "resource:generated", status: "success" } })];

        expect(mergeGenerationTaskResultNodes(current, base, applied, task(), "node-1")).toBe(current);
    });
});

function videoNode(assetId: string, storageKey: string): CanvasNodeData {
    return {
        id: "node-1",
        type: CanvasNodeType.Video,
        title: "镜头",
        position: { x: 0, y: 0 },
        width: 320,
        height: 180,
        metadata: { assetId, storageKey, content: "https://example.test/old.mp4", prompt: "旧提示词" },
    };
}

describe("applyGeneratedMediaResultMetadata", () => {
    test("clears the previous asset binding when a regenerated media result lands", () => {
        const node = videoNode("asset-old", "video:old");
        const next = applyGeneratedMediaResultMetadata(node, videoMetadata({
            url: "https://example.test/new.mp4",
            storageKey: "video:new",
            width: 1280,
            height: 720,
            bytes: 12,
            mimeType: "video/mp4",
            durationMs: 4000,
        }), { prompt: "新提示词" });

        expect(next.assetId).toBeUndefined();
        expect(next.storageKey).toBe("video:new");
        expect(next.content).toBe("https://example.test/new.mp4");
        expect(next.prompt).toBe("新提示词");
        expect(next.status).toBe("success");
    });
});
