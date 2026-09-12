import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { canvasConnectionSnapKind } from "../src/lib/canvas/canvas-connection-snap";

describe("canvas connection card snapping", () => {
    const node = { position: { x: 100, y: 200 }, width: 320, height: 180 };
    const leftAnchor = { x: 100, y: 290 };

    test("accepts every point inside an ordinary node card", () => {
        expect(canvasConnectionSnapKind({ x: 101, y: 201 }, node, leftAnchor, 56)).toBe("body");
        expect(canvasConnectionSnapKind({ x: 100, y: 290 }, node, leftAnchor, 56)).toBe("body");
        expect(canvasConnectionSnapKind({ x: 260, y: 290 }, node, leftAnchor, 56)).toBe("body");
        expect(canvasConnectionSnapKind({ x: 419, y: 379 }, node, leftAnchor, 56)).toBe("body");
    });

    test("keeps the existing circular side-handle snap zone", () => {
        expect(canvasConnectionSnapKind({ x: 50, y: 290 }, node, leftAnchor, 56)).toBe("handle");
        expect(canvasConnectionSnapKind({ x: 35, y: 290 }, node, leftAnchor, 56)).toBeNull();
    });

    test("can require a precise handle for row-based nodes", () => {
        expect(canvasConnectionSnapKind({ x: 260, y: 290 }, node, leftAnchor, 56, false)).toBeNull();
        expect(canvasConnectionSnapKind({ x: 100, y: 290 }, node, leftAnchor, 56, false)).toBe("handle");
    });

    test("keeps body-drop previews following the pointer while retaining upstream reference replacement", () => {
        const source = readFileSync(new URL("../src/pages/canvas/use-canvas-connection-controller.ts", import.meta.url), "utf8");
        const finish = source.slice(source.indexOf("const finishConnection ="), source.indexOf("const handleConnectStart ="));
        const replacement = finish.indexOf("onReplaceReference(targetNodeId");
        const ordinaryDrop = finish.indexOf("const dropTarget = getConnectionDropTarget");
        expect(replacement).toBeGreaterThan(0);
        expect(ordinaryDrop).toBeGreaterThan(replacement);
        expect(finish).toContain("connectNodes(currentConnection, dropTarget.nodeId, dropTarget.handleId, dropTarget.anchorRatio)");
        expect(source).toContain("setConnectionPreviewTargetNodeId(dropTarget.snapToHandle ? dropTarget.nodeId : null)");
        expect(source).toContain("setMouseWorld(point)");
        expect(source).toContain("updateConnectionReplaceHover(null)");
    });
});
