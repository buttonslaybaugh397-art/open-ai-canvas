import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { captureVideoPoster } from "@/lib/video-poster";

let originals: Map<string, PropertyDescriptor | undefined>;
let videos: Array<{ crossOrigin: string; src: string; corsAtLoad: string }>;

beforeEach(() => {
    videos = [];
    originals = new Map(["window", "document"].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
    Object.defineProperty(globalThis, "window", { configurable: true, value: {
        location: new URL("https://app.example/canvas/draft"), setTimeout, clearTimeout,
    } });
    Object.defineProperty(globalThis, "document", { configurable: true, value: {
        createElement: (tag: string) => {
            if (tag === "canvas") return {
                width: 0, height: 0,
                getContext: () => ({ fillStyle: "", fillRect() {}, drawImage() {} }),
                toBlob: (callback: (blob: Blob) => void) => callback(new Blob(["poster"], { type: "image/jpeg" })),
            };
            const video = {
                crossOrigin: "", src: "", corsAtLoad: "", preload: "", muted: false, playsInline: false,
                videoWidth: 640, videoHeight: 360, duration: 2, readyState: 3,
                onloadedmetadata: null as null | (() => void), onloadeddata: null as null | (() => void), onerror: null,
                pause() {},
                removeAttribute() { this.src = ""; },
                load() {
                    if (!this.src) return;
                    this.corsAtLoad = this.crossOrigin;
                    queueMicrotask(() => { this.onloadedmetadata?.(); this.onloadeddata?.(); });
                },
            };
            videos.push(video);
            return video;
        },
    } });
});

afterEach(() => {
    for (const [key, descriptor] of originals) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor);
        else Reflect.deleteProperty(globalThis, key);
    }
});

describe("video poster CDN redirects", () => {
    for (const source of ["/api/resources/video/file", "/api/admin/resources/video/file", "https://app.example/local.mp4", "https://cdn.example/video.mp4"]) {
        test(`enables anonymous CORS before HTTP media load: ${source}`, async () => {
            const result = await captureVideoPoster(source);
            expect(videos[0].corsAtLoad).toBe("anonymous");
            expect(result.poster?.type).toBe("image/jpeg");
            expect(result.width).toBe(640);
        });
    }
    for (const source of ["blob:https://app.example/local", "data:video/mp4;base64,AAAA"]) {
        test(`keeps local source decoding unchanged: ${source}`, async () => {
            const result = await captureVideoPoster(source);
            expect(videos[0].corsAtLoad).toBe("");
            expect(result.poster?.size).toBeGreaterThan(0);
        });
    }
});
