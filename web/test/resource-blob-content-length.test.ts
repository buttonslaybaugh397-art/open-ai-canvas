import { afterEach, expect, spyOn, test } from "bun:test";

import { getResourceBlob } from "../src/services/api/resources";
import { http } from "../src/services/api/request";

const originalFetch = globalThis.fetch;
let resolveSpy: ReturnType<typeof spyOn> | undefined;

afterEach(() => {
    resolveSpy?.mockRestore();
    globalThis.fetch = originalFetch;
});

test("rejects a truncated CDN response when Content-Length does not match", async () => {
    resolveSpy = spyOn(http, "get").mockResolvedValue({ url: "https://cdn.example/video.mp4" });
    globalThis.fetch = (async () =>
        new Response("short", {
            status: 200,
            headers: { "Content-Type": "video/mp4", "Content-Length": "10" },
        })) as typeof fetch;

    await expect(getResourceBlob("resource:video")).rejects.toMatchObject({ status: 502, retryable: true });
});

test("accepts a complete CDN response when Content-Length matches", async () => {
    resolveSpy = spyOn(http, "get").mockResolvedValue({ url: "https://cdn.example/video.mp4" });
    globalThis.fetch = (async () =>
        new Response("complete", {
            status: 200,
            headers: { "Content-Type": "video/mp4", "Content-Length": "8" },
        })) as typeof fetch;

    expect(await (await getResourceBlob("resource:video"))?.text()).toBe("complete");
});
