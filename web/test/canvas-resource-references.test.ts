import { describe, expect, test } from "bun:test";

import { buildNodeMentionReferences, collectUpstreamVideoNodes } from "../src/lib/canvas/canvas-resource-references";
import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "../src/types/canvas";

function videoNode(id: string): CanvasNodeData {
    return {
        id,
        type: CanvasNodeType.Video,
        title: id,
        position: { x: 0, y: 0 },
        width: 100,
        height: 100,
        metadata: { content: `data:video/mp4;base64,${id}` },
    };
}

function textNode(id: string): CanvasNodeData {
    return {
        id,
        type: CanvasNodeType.Text,
        title: id,
        position: { x: 0, y: 0 },
        width: 100,
        height: 60,
        metadata: { content: id },
    };
}

function connection(fromNodeId: string, toNodeId: string): CanvasConnection {
    return { id: `conn-${fromNodeId}-${toNodeId}`, fromNodeId, toNodeId };
}

describe("collectUpstreamVideoNodes", () => {
    test("下游视频节点能回溯到上游视频源", () => {
        const source = videoNode("source-video");
        const segment = videoNode("segment-video");
        const target = videoNode("target-video");
        const text = textNode("script");
        const nodes = [target, segment, source, text];
        const connections = [connection("source-video", "segment-video"), connection("segment-video", "target-video"), connection("script", "segment-video")];
        expect(collectUpstreamVideoNodes("target-video", nodes, connections).map((node) => node.id)).toEqual(["target-video", "segment-video", "source-video"]);
    });

    test("存在环时不会死循环", () => {
        const a = videoNode("a");
        const b = videoNode("b");
        const nodes = [a, b];
        const connections = [connection("a", "b"), connection("b", "a")];
        expect(collectUpstreamVideoNodes("a", nodes, connections).length).toBe(2);
    });
});

describe("buildNodeMentionReferences", () => {
    test("成功视频节点不会把自身结果标记为活动参考视频", () => {
        const result = videoNode("generated-video");

        expect(buildNodeMentionReferences(result, [result], [])).toEqual([]);
    });

    test("成功视频节点仍保留真实上游参考视频", () => {
        const source = videoNode("reference-video");
        const result = videoNode("generated-video");

        expect(buildNodeMentionReferences(result, [source, result], [connection(source.id, result.id)]).map((reference) => reference.nodeId)).toEqual([source.id]);
    });
});
