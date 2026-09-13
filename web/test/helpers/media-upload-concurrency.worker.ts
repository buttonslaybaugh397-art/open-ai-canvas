import localforage from "localforage";
import { apiClient, ApiError } from "../../src/services/api/request";

function deferred() {
    let resolve!: () => void;
    const promise = new Promise<void>((done) => {
        resolve = done;
    });
    return { promise, resolve };
}

async function run(scenario: string) {
    localforage.createInstance = (() => ({
        getItem: async () => null,
        setItem: async (_key: string, value: unknown) => {
            if (scenario === "local-failure") throw new Error("local storage failed");
            return value;
        },
    })) as typeof localforage.createInstance;
    Object.defineProperty(globalThis, "window", {
        configurable: true,
        value: {
            localStorage: { getItem: () => null },
            addEventListener() {},
            removeEventListener() {},
            clearTimeout,
            setTimeout,
            location: { href: "http://localhost/", origin: "http://localhost" },
        },
    });
    Object.defineProperty(globalThis, "document", {
        configurable: true,
        value: {
            createElement: (tag: string) => {
                if (tag === "canvas")
                    return {
                        getContext: () => ({ fillRect() {}, drawImage() {} }),
                        toBlob: (done: (blob: Blob) => void) => done(new Blob(["poster"], { type: "image/jpeg" })),
                    };
                if (tag !== "video") throw new Error(`unexpected element: ${tag}`);
                return {
                    videoWidth: 1280,
                    videoHeight: 720,
                    duration: 2,
                    mozHasAudio: true,
                    onloadedmetadata: null as (() => void) | null,
                    onloadeddata: null as (() => void) | null,
                    pause() {},
                    removeAttribute() {},
                    load() {
                        queueMicrotask(() => {
                            this.onloadedmetadata?.();
                            this.onloadeddata?.();
                        });
                    },
                };
            },
        },
    });
    Object.defineProperty(globalThis, "Image", {
        configurable: true,
        value: class {
            naturalWidth = 400;
            naturalHeight = 225;
            onload?: () => void;
            set src(_value: string) {
                queueMicrotask(() => this.onload?.());
            }
        },
    });
    const created: string[] = [];
    const revoked: string[] = [];
    const create = URL.createObjectURL.bind(URL);
    const revoke = URL.revokeObjectURL.bind(URL);
    URL.createObjectURL = (blob) => {
        const url = create(blob);
        created.push(url);
        return url;
    };
    URL.revokeObjectURL = (url) => {
        revoked.push(url);
        revoke(url);
    };
    console.warn = () => {};

    const posterStarted = deferred();
    const videoStarted = deferred();
    const releasePoster = deferred();
    let posterFinished = false;
    let overlapped = false;
    apiClient.defaults.adapter = async (config) => {
        const kind = (config.data as FormData).get("kind");
        if (kind === "image") {
            posterStarted.resolve();
            await releasePoster.promise;
            posterFinished = true;
            if (scenario === "poster-failure") throw new ApiError("poster denied", { status: 403 });
        } else if (kind === "video") {
            overlapped = !posterFinished;
            videoStarted.resolve();
            if (scenario === "video-failure") throw new ApiError("video denied", { status: 403 });
            if (scenario === "local-failure") throw new ApiError("network failure", { status: 503 });
        } else throw new Error(`unexpected kind: ${kind}`);
        return {
            config,
            status: 200,
            statusText: "OK",
            headers: {},
            data: {
                code: 0,
                data: {
                    resource: { id: `${kind}-result`, size: 7, mimeType: kind === "image" ? "image/jpeg" : "video/mp4" },
                },
            },
        };
    };
    const { uploadMediaFile } = await import("../../src/services/file-storage");
    let settled = false;
    const pending = uploadMediaFile(new File(["payload"], "test.mp4", { type: "video/mp4" })).then(
        (value) => {
            settled = true;
            return { value };
        },
        (error) => {
            settled = true;
            return { error };
        },
    );
    await Promise.all([posterStarted.promise, videoStarted.promise]);
    await new Promise((resolve) => setTimeout(resolve, 0));
    const settledBeforePoster = settled;
    releasePoster.resolve();
    const result = await pending;
    return {
        overlapped,
        settledBeforePoster,
        previewReleased: revoked.includes(created[0]),
        ...("value" in result
            ? {
                  storageKey: result.value.storageKey,
                  posterKey: result.value.preview?.storageKey,
                  pendingRemoteUpload: result.value.pendingRemoteUpload,
              }
            : { error: result.error.message, permanent: result.error.permanent }),
    };
}

self.onmessage = async (event: MessageEvent<string>) => {
    try {
        self.postMessage({ ok: true, result: await run(event.data) });
    } catch (error) {
        self.postMessage({ ok: false, error: error instanceof Error ? error.stack : String(error) });
    }
};
