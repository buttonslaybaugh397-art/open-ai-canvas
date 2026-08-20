import { describe, expect, test } from "bun:test";

import { isolateCopiedNodeMetadata } from "../src/lib/canvas/canvas-node-copy";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

describe("isolateCopiedNodeMetadata", () => {
    test("视频副本只保留生成配置和参考关系，不继承输出媒体", () => {
        const source: CanvasNodeData = {
            id: "video-source",
            type: CanvasNodeType.Video,
            title: "成片",
            position: { x: 0, y: 0 },
            width: 640,
            height: 360,
            metadata: {
                content: "https://example.com/result.mp4",
                previewContent: "https://example.com/preview.mp4",
                storageKey: "resource:video-result",
                mimeType: "video/mp4",
                bytes: 1024,
                durationMs: 5000,
                naturalWidth: 1280,
                naturalHeight: 720,
                assetId: "asset-result",
                prompt: "镜头缓慢推进",
                model: "video-model",
                size: "16:9",
                seconds: "5",
                references: ["resource:image-reference"],
                videoEditOperation: "image_to_video",
                videoStartFrameNodeId: "image-reference",
                status: "success",
                taskId: "task-result",
                generationEffectKeys: ["effect-result"],
                agentGenerationContinuation: { id: "continuation", taskId: "task-result", status: "completed" },
                versionOfNodeId: "video-source",
                versionLabel: "A",
                versionPrimary: true,
            },
        };

        const metadata = isolateCopiedNodeMetadata(source, new Map());

        expect(metadata).toMatchObject({
            prompt: "镜头缓慢推进",
            model: "video-model",
            size: "16:9",
            seconds: "5",
            references: ["resource:image-reference"],
            videoEditOperation: "image_to_video",
            videoStartFrameNodeId: "image-reference",
            status: "idle",
            copiedFromNodeId: "video-source",
        });
        expect(metadata.content).toBeUndefined();
        expect(metadata.previewContent).toBeUndefined();
        expect(metadata.storageKey).toBeUndefined();
        expect(metadata.assetId).toBeUndefined();
        expect(metadata.taskId).toBeUndefined();
        expect(metadata.generationEffectKeys).toBeUndefined();
        expect(metadata.agentGenerationContinuation).toBeUndefined();
        expect(metadata.versionOfNodeId).toBeUndefined();
        expect(metadata.versionLabel).toBeUndefined();
        expect(metadata.versionPrimary).toBeUndefined();
    });

    test("普通参考视频副本仍保留原媒体", () => {
        const source: CanvasNodeData = {
            id: "uploaded-video",
            type: CanvasNodeType.Video,
            title: "上传视频",
            position: { x: 0, y: 0 },
            width: 640,
            height: 360,
            metadata: { content: "https://example.com/reference.mp4", storageKey: "resource:reference-video", mimeType: "video/mp4", status: "success" },
        };

        const metadata = isolateCopiedNodeMetadata(source, new Map());

        expect(metadata.content).toBe(source.metadata?.content);
        expect(metadata.storageKey).toBe(source.metadata?.storageKey);
        expect(metadata.status).toBe("success");
    });
});
