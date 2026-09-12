import type { Position } from "@/types/canvas";

type SnapNodeBounds = {
    position: Position;
    width: number;
    height: number;
};

export type CanvasConnectionSnapKind = "handle" | "body" | null;

export function canvasConnectionSnapKind(
    point: Position,
    node: SnapNodeBounds,
    anchor: Position,
    handleRadius: number,
    includeNodeBody = true,
) : CanvasConnectionSnapKind {
    const insideNode = includeNodeBody
        && point.x >= node.position.x
        && point.x <= node.position.x + node.width
        && point.y >= node.position.y
        && point.y <= node.position.y + node.height;
    if (insideNode) return "body";

    const dx = point.x - anchor.x;
    const dy = point.y - anchor.y;
    return dx * dx + dy * dy <= handleRadius * handleRadius ? "handle" : null;
}
