import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { downloadMediaFile } from "@/services/media-download";

class ElementStub {
    hidden = false;
    title = "";
    src = "";
    href = "";
    download = "";
    referrerPolicy = "";
    style = { display: "" };
    attributes = new Map<string, string>();
    onload?: () => void;
    contentWindow = { location: { href: "about:blank" } };
    removed = false;
    click = mock(() => {});
    constructor(readonly tag: string) {}
    setAttribute(name: string, value: string) { this.attributes.set(name, value); }
    remove() { this.removed = true; }
}

let elements: ElementStub[];
let errors: Error[];
let timers: Map<number, () => void>;
let fetchMock: ReturnType<typeof mock>;
let originals: Map<string, PropertyDescriptor | undefined>;

beforeEach(() => {
    elements = [];
    errors = [];
    timers = new Map();
    originals = new Map(["window", "document", "fetch"].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
    let timerId = 0;
    fetchMock = mock(async () => new Response("image data", { headers: { "Content-Type": "image/png" } }));
    Object.defineProperty(globalThis, "fetch", { configurable: true, writable: true, value: fetchMock });
    Object.defineProperty(globalThis, "window", { configurable: true, value: {
        location: new URL("https://app.example/canvas/draft"),
        setTimeout: (callback: () => void) => { timers.set(++timerId, callback); return timerId; },
        clearTimeout: (id: number) => timers.delete(id),
    } });
    Object.defineProperty(globalThis, "document", { configurable: true, value: {
        createElement: (tag: string) => new ElementStub(tag),
        body: { appendChild: (element: ElementStub) => elements.push(element) },
    } });
});

afterEach(() => {
    for (const timer of [...timers.values()]) timer();
    for (const [key, descriptor] of originals) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor);
        else Reflect.deleteProperty(globalThis, key);
    }
});

function start(url = "/api/resources/video/file?direct=1&download=1") {
    downloadMediaFile(url, "final.png", error => errors.push(error));
    return elements[0];
}

async function loadInline(frame: ElementStub) {
    Object.defineProperty(frame, "contentWindow", { get: () => { throw new DOMException("cross origin", "SecurityError"); } });
    frame.onload?.();
    frame.onload?.();
    for (let i = 0; i < 30 && !frame.removed; i++) await new Promise(resolve => setTimeout(resolve, 0));
}

describe("media download navigation isolation", () => {
    test("native CDN attachments do not fetch a Blob or navigate a top-level link", () => {
        const frame = start();
        expect(frame.tag).toBe("iframe");
        expect(frame.hidden).toBe(true);
        expect(frame.src).toBe("https://app.example/api/resources/video/file?direct=1&download=1");
        expect(frame.attributes.get("sandbox")).toBe("allow-downloads allow-same-origin");
        expect(frame.referrerPolicy).toBe("no-referrer");
        frame.onload?.(); // Initial blank document must not trigger a fallback.
        expect(fetchMock).not.toHaveBeenCalled();
        expect(elements).toHaveLength(1);
        expect(errors).toHaveLength(0);
    });

    test("inline CDN media falls back once to a local object URL download", async () => {
        const frame = start();
        await loadInline(frame);
        expect(fetchMock).toHaveBeenCalledTimes(1);
        expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({ credentials: "same-origin", referrerPolicy: "no-referrer" });
        expect(elements).toHaveLength(2);
        expect(elements[1].href).toStartWith("blob:");
        expect(elements[1].download).toBe("final.png");
        expect(elements[1].click).toHaveBeenCalledTimes(1);
        expect(elements[1].removed).toBe(true);
        expect(frame.removed).toBe(true);
        expect(errors).toHaveLength(0);
    });

    test("404 responses surface a failure without downloading an error document", async () => {
        fetchMock.mockImplementation(async () => new Response("missing", { status: 404 }));
        await loadInline(start());
        expect(errors[0]?.message).toContain("404");
        expect(elements).toHaveLength(1);
        expect(elements[0].removed).toBe(true);
    });

    test("CORS failure is reported and never falls back to opening the CDN", async () => {
        fetchMock.mockImplementation(async () => { throw new TypeError("Failed to fetch"); });
        await loadInline(start("https://cdn.example/video.mp4"));
        expect(errors[0]?.message).toContain("CORS");
        expect(elements).toHaveLength(1);
    });

    test("HTML login/error pages are not saved as media", async () => {
        fetchMock.mockImplementation(async () => new Response("<html>login</html>", { headers: { "Content-Type": "text/html; charset=utf-8" } }));
        await loadInline(start());
        expect(errors).toHaveLength(1);
        expect(elements).toHaveLength(1);
    });

    test("local Blob and data URLs download directly", () => {
        for (const url of ["blob:https://app.example/local", "data:image/png;base64,AAAA"]) {
            start(url);
        }
        expect(elements.every(element => element.tag === "a" && element.removed)).toBe(true);
        expect(fetchMock).not.toHaveBeenCalled();
    });

    test("unsupported protocols are rejected before creating a browsing context", () => {
        start("javascript:alert(1)");
        expect(errors).toHaveLength(1);
        expect(elements).toHaveLength(0);
    });

    test("concurrent downloads have independent frames", () => {
        start("/api/resources/first/file?download=1");
        start("/api/resources/second/file?download=1");
        expect(elements).toHaveLength(2);
        expect(elements[0].src).not.toBe(elements[1].src);
        expect(elements.every(element => !element.removed)).toBe(true);
    });
});
