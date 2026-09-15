import { describe, expect, test } from "bun:test";

import { resourceDownloadUrl } from "@/services/api/resources";

describe("resource download URL", () => {
    test("requests direct streaming with attachment semantics", () => {
        expect(resourceDownloadUrl("resource/with space")).toBe("/api/resources/resource%2Fwith%20space/file?direct=1&download=1");
    });
    test("encodes the canvas download name without allowing query injection", () => {
        const url = new URL(resourceDownloadUrl("media", "画布_镜头&1.png"), "https://example.test");
        expect(url.searchParams.get("filename")).toBe("画布_镜头&1.png");
        expect(url.searchParams.get("download")).toBe("1");
    });
});
