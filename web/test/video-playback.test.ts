import { afterEach, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { ApiError, apiClient } from "../src/services/api/request";
import { requestResourcePlayback, resourceIdFromFileUrl } from "../src/services/api/resources";
import { createVideoPlaybackResolver, createVideoPlaybackSession, isVideoDecodeError, videoPlaybackResourceId, type VideoPlaybackState } from "../src/services/video-playback";

const ready = (id = "video", playbackStatus = "ready") => ({ id, status: "ready", playbackStatus });
const flush = async () => {
    for (let i = 0; i < 15; i++) await Promise.resolve();
};
const cleanups: (() => void)[] = [];
const originalAdapter = apiClient.defaults.adapter;
afterEach(() => {
    cleanups.splice(0).forEach((cleanup) => cleanup());
    apiClient.defaults.adapter = originalAdapter;
});

function harness(overrides: Partial<Parameters<typeof createVideoPlaybackResolver>[0]> = {}) {
    let calls = 0;
    let refreshes = 0;
    let scope = "alice";
    const deps = {
        request: async (id: string) => {
            calls++;
            return ready(id, "processing");
        },
        refresh: async (id: string) => {
            refreshes++;
            return ready(id);
        },
        scope: () => scope,
        variantUrl: (id: string) => `/api/resources/${id}/file?variant=playback`,
        sleep: async () => {},
        ...overrides,
    };
    const resolver = createVideoPlaybackResolver(deps);
    const changes: VideoPlaybackState[] = [];
    const session = createVideoPlaybackSession("blob:original", "resource:video", (state) => changes.push(state), resolver, deps.scope);
    cleanups.push(session.dispose);
    return {
        session,
        resolver,
        changes,
        calls: () => calls,
        refreshes: () => refreshes,
        switchScope: (next: string) => {
            scope = next;
        },
    };
}

test("native playback does not probe or transcode; only decode/source errors qualify", () => {
    const h = harness();
    expect(h.session.snapshot().src).toBe("blob:original");
    expect(h.calls()).toBe(0);
    expect([undefined, 0, 1, 2, 3, 4, 5].map(isVideoDecodeError)).toEqual([false, false, false, false, true, true, false]);
});

test("one decoder failure requests once, waits for ready and preserves original input", async () => {
    const h = harness();
    expect(h.session.handleError(3)).toBe(true);
    expect(h.session.handleError(4)).toBe(false);
    expect(h.session.snapshot().phase).toBe("preparing");
    expect(h.session.snapshot().src).toBe("blob:original");
    await flush();
    expect(h.calls()).toBe(1);
    expect(h.refreshes()).toBe(1);
    expect(h.session.snapshot()).toEqual({ src: "/api/resources/video/file?variant=playback", phase: "compatible", message: "" });
});

test("network and aborted errors do not start compatibility work", async () => {
    const h = harness();
    h.session.handleError(1);
    expect(h.session.snapshot().phase).toBe("original");
    h.session.handleError(2);
    await flush();
    expect(h.session.snapshot().phase).toBe("failed");
    expect(h.session.snapshot().message).toContain("网络");
    expect(h.calls()).toBe(0);
});

test("ready variants are reused and failures never loop back to original", async () => {
    const h = harness({ request: async (id) => ready(id) });
    h.session.handleError(4);
    await flush();
    expect(h.resolver.cached("video")).toContain("variant=playback");
    const next = createVideoPlaybackSession(
        "blob:next",
        "resource:video",
        () => {},
        h.resolver,
        () => "alice",
    );
    cleanups.push(next.dispose);
    expect(next.snapshot().phase).toBe("compatible");
    next.handleError(3);
    next.handleError(3);
    expect(next.snapshot().phase).toBe("failed");
    expect(h.resolver.cached("video")).toBe("");
    expect(h.refreshes()).toBe(0);
});

for (const status of ["failed", "none", ""]) {
    test(`backend ${status || "empty"} status is terminal, never endless polling`, async () => {
        const h = harness({ request: async (id) => ready(id, status) });
        h.session.handleError(3);
        await flush();
        expect(h.session.snapshot().phase).toBe("failed");
        expect(h.refreshes()).toBe(0);
        h.session.handleError(3);
        expect(h.session.snapshot().phase).toBe("failed");
    });
}

test("raw/local videos without resource identity explain the synchronization requirement", () => {
    const h = harness();
    const session = createVideoPlaybackSession("blob:local", undefined, () => {}, h.resolver);
    cleanups.push(session.dispose);
    session.handleError(3);
    expect(session.snapshot().message).toContain("同步到云端");
    expect(h.calls()).toBe(0);
});

test("resource identity uses only the configured backend, not a lookalike third-party path", () => {
    expect(videoPlaybackResourceId("blob:video", "resource:saved")).toBe("saved");
    expect(resourceIdFromFileUrl("/api/resources/video/file?proxy=1")).toBe("video");
    expect(resourceIdFromFileUrl("/api/resources/video/file?variant=playback")).toBe("video");
    expect(resourceIdFromFileUrl("https://evil.example/api/resources/video/file")).toBe("");
    expect(resourceIdFromFileUrl("/api/resources/extra/video/file")).toBe("");
    expect(resourceIdFromFileUrl("/api/resources/%/file")).toBe("");
});

test("concurrent views share requests; cancelling one does not cancel another", async () => {
    let release!: () => void;
    let serverSignal!: AbortSignal;
    let calls = 0;
    const h = harness({
        request: async (id, signal) => {
            calls++;
            serverSignal = signal;
            await new Promise<void>((resolve) => {
                release = resolve;
            });
            return ready(id);
        },
    });
    const first = new AbortController();
    const second = new AbortController();
    const a = h.resolver.resolve("video", first.signal).catch((error: Error) => error.name);
    const b = h.resolver.resolve("video", second.signal);
    first.abort();
    expect(serverSignal.aborted).toBe(false);
    release();
    expect(await a).toBe("AbortError");
    expect(await b).toContain("variant=playback");
    expect(calls).toBe(1);
});

test("disposing last view aborts HTTP and ignores a late response", async () => {
    let release!: () => void;
    let serverSignal!: AbortSignal;
    const h = harness({
        request: async (id, signal) => {
            serverSignal = signal;
            await new Promise<void>((resolve) => {
                release = resolve;
            });
            return ready(id);
        },
    });
    h.session.handleError(3);
    h.session.dispose();
    expect(serverSignal.aborted).toBe(true);
    release();
    await flush();
    expect(h.changes.map((change) => change.phase)).toEqual(["preparing"]);
    expect(h.resolver.cached("video")).toBe("");
});

test("account switch cannot publish a prior account's result or cache", async () => {
    let release!: () => void;
    const h = harness({
        request: async (id) => {
            await new Promise<void>((resolve) => {
                release = resolve;
            });
            return ready(id);
        },
    });
    h.session.handleError(3);
    h.switchScope("bob");
    release();
    await flush();
    expect(h.changes.map((change) => change.phase)).toEqual(["preparing"]);
    expect(h.resolver.cached("video")).toBe("");
});

test("polls are serialized and bounded", async () => {
    let polls = 0;
    const h = harness({
        refresh: async (id) => {
            polls++;
            return ready(id, "processing");
        },
    });
    const controller = new AbortController();
    await expect(h.resolver.resolve("video", controller.signal)).rejects.toThrow("超时");
    expect(polls).toBe(120);
});

test("the final allowed poll can still return a ready variant", async () => {
    let polls = 0;
    const h = harness({ refresh: async (id) => ready(id, ++polls === 120 ? "ready" : "processing") });
    await expect(h.resolver.resolve("video", new AbortController().signal)).resolves.toContain("variant=playback");
    expect(polls).toBe(120);
});

test("Vidstack source-element failures have an explicit capture path", () => {
    const source = readFileSync(new URL("../src/components/video-player.tsx", import.meta.url), "utf8");
    expect(source).toMatch(/<MediaProvider\s+onErrorCapture=/);
    expect(source).toContain("event.target instanceof HTMLSourceElement");
    expect(source).toContain("media instanceof HTMLVideoElement");
    expect(source).toContain('playback.phase === "compatible" ? "video/mp4"');
});

test("production backend includes the encoder used by preview adaptation", () => {
    const dockerfile = readFileSync(new URL("../../backend/Dockerfile", import.meta.url), "utf8");
    const runtime = dockerfile.slice(dockerfile.lastIndexOf("FROM alpine:"));
    expect(runtime).toMatch(/apk add --no-cache[^\r\n]*\bffmpeg\b/);
});

test("transient polling failure retries at most four times", async () => {
    let polls = 0;
    const h = harness({
        refresh: async () => {
            polls++;
            throw new ApiError("unavailable", { status: 503 });
        },
    });
    await expect(h.resolver.resolve("video", new AbortController().signal)).rejects.toThrow("暂不可达");
    expect(polls).toBe(4);
});

for (const status of [401, 403, 404, 429]) {
    test(`polling HTTP ${status} stops immediately without resubmission`, async () => {
        let polls = 0;
        const h = harness({
            refresh: async () => {
                polls++;
                throw new ApiError("denied", { status });
            },
        });
        h.session.handleError(3);
        await flush();
        expect(h.session.snapshot().phase).toBe("failed");
        expect(h.calls()).toBe(1);
        expect(polls).toBe(1);
    });
}

test("playback API preserves authentication, signal and business envelope failure", async () => {
    const controller = new AbortController();
    apiClient.defaults.adapter = async (config) => {
        expect(config.method).toBe("post");
        expect(config.url).toBe("/resources/video/playback");
        expect(config.withCredentials).toBe(true);
        expect(config.signal).toBe(controller.signal);
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 1, data: { resource: ready() }, msg: "failed" } };
    };
    await expect(requestResourcePlayback("video", controller.signal)).rejects.toThrow("failed");
});
