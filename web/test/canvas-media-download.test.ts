import { describe, expect, test } from "bun:test";

import { canvasMediaDownloadFileName } from "../src/lib/canvas/canvas-project-generation";
import { CanvasNodeType } from "../src/types/canvas";

describe("canvas media download filenames", () => {
    test("uses the canvas card title and preserves the media extension", () => {
        expect(canvasMediaDownloadFileName({ id: "node-1", type: CanvasNodeType.Image, title: "夜景海报", metadata: { content: "data:image/jpeg;base64,AAAA" } })).toBe("夜景海报.jpeg");
        expect(canvasMediaDownloadFileName({ id: "node-2", type: CanvasNodeType.Video, title: "片段一", metadata: { content: "blob:video-1" } })).toBe("片段一.mp4");
    });

    test("cleans unsafe or empty titles and keeps a stable fallback", () => {
        expect(canvasMediaDownloadFileName({ id: "node-3", type: CanvasNodeType.Image, title: "  海报:/终稿.  ", metadata: { content: "data:image/png;base64,AAAA" } })).toBe("海报__终稿.png");
        expect(canvasMediaDownloadFileName({ id: "node-4", type: CanvasNodeType.Video, title: "   ", metadata: { content: "blob:video-2" } })).toBe("canvas-video-node-4.mp4");
    });
});
