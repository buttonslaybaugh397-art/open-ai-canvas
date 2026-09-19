import { afterEach, expect, spyOn, test } from "bun:test";
import { getResourceBlob, resourceFileUrl } from "../src/services/api/resources";
import { ApiError, http } from "../src/services/api/request";

const originalFetch = globalThis.fetch;
let resolveSpy: ReturnType<typeof spyOn> | undefined;
afterEach(() => {
    resolveSpy?.mockRestore();
    globalThis.fetch = originalFetch;
});

test("reads CDN bytes without cookies after an authenticated URL lookup", async () => {
    const url = "https://cdn.example/video.mp4?signature=test";
    resolveSpy = spyOn(http, "get").mockResolvedValue({ url });
    const signal = new AbortController().signal;
    let calls = 0;
    globalThis.fetch = (async (target, options) => {
        calls++;
        expect(target).toBe(url);
        expect(options?.credentials).toBe("omit");
        expect(options?.redirect).toBe("error");
        expect(options?.referrerPolicy).toBe("no-referrer");
        expect(options?.signal).toBe(signal);
        return new Response("clip", { headers: { "Content-Type": "video/mp4" } });
    }) as typeof fetch;
    const blob = await getResourceBlob("resource:video", signal);
    expect(await blob?.text()).toBe("clip");
    expect(resolveSpy).toHaveBeenCalledWith("/resources/video/file", { params: { resolve: "1" }, signal });
    expect(calls).toBe(1);
});

test("retains authenticated reads only when the backend explicitly selects local delivery", async () => {
    resolveSpy = spyOn(http, "get").mockResolvedValue({ url: "" });
    globalThis.fetch = (async (url, options) => {
        expect(url).toBe(resourceFileUrl("local"));
        expect(options?.credentials).toBe("include");
        expect(options?.redirect).toBe("error");
        return new Response("local");
    }) as typeof fetch;
    expect(await (await getResourceBlob("resource:local"))?.text()).toBe("local");
});

test("CDN CORS failures never start a backend proxy download", async () => {
    resolveSpy = spyOn(http, "get").mockResolvedValue({ url: "https://cdn.example/video.mp4" });
    let calls = 0;
    globalThis.fetch = (async () => {
        calls++;
        throw new TypeError("Failed to fetch");
    }) as typeof fetch;
    await expect(getResourceBlob("resource:video")).rejects.toThrow("CORS");
    expect(calls).toBe(1);
});

test("admin lookup uses the admin authorization endpoint", async () => {
    resolveSpy = spyOn(http, "get").mockResolvedValue({ url: "https://cdn.example/video.mp4" });
    globalThis.fetch = (async () => new Response("clip")) as typeof fetch;
    await getResourceBlob("resource:video", undefined, "admin");
    expect(resolveSpy).toHaveBeenCalledWith("/admin/resources/video/file", { params: { resolve: "1" }, signal: undefined });
});

test("unexpected CDN redirects fail instead of reading the redirect target", async () => {
    resolveSpy = spyOn(http, "get").mockResolvedValue({ url: "https://cdn.example/video.mp4" });
    let calls = 0;
    globalThis.fetch = (async (_url, options) => {
        expect(options?.redirect).toBe("error");
        calls++;
        throw new TypeError("Unexpected redirect");
    }) as typeof fetch;
    await expect(getResourceBlob("resource:video")).rejects.toThrow("CORS");
    expect(calls).toBe(1);
});

test("unavailable delivery and authorization errors do not read media", async () => {
    resolveSpy = spyOn(http, "get");
    globalThis.fetch = (() => { throw new Error("must not fetch media"); }) as typeof fetch;
    for (const status of [401, 403, 404, 503]) {
        const error = new ApiError("unavailable", { status });
        resolveSpy.mockRejectedValueOnce(error);
        await expect(getResourceBlob("resource:video")).rejects.toBe(error);
    }
});

test("CDN HTTP failures remain errors and cancellation is preserved", async () => {
    resolveSpy = spyOn(http, "get").mockResolvedValue({ url: "https://cdn.example/video.mp4" });
    globalThis.fetch = (async () => new Response("missing", { status: 404 })) as typeof fetch;
    await expect(getResourceBlob("resource:video")).rejects.toMatchObject({ status: 404 });
    const controller = new AbortController();
    const error = new DOMException("Cancelled", "AbortError");
    globalThis.fetch = (async () => { controller.abort(); throw error; }) as typeof fetch;
    await expect(getResourceBlob("resource:video", controller.signal)).rejects.toBe(error);
});
