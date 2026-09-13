import { expect, test } from "bun:test";

type Result = {
    overlapped: boolean;
    settledBeforePoster: boolean;
    previewReleased: boolean;
    storageKey?: string;
    posterKey?: string;
    error?: string;
    permanent?: boolean;
    pendingRemoteUpload?: boolean;
};

function runScenario(scenario: string): Promise<Result> {
    return new Promise((resolve, reject) => {
        const worker = new Worker(new URL("./helpers/media-upload-concurrency.worker.ts", import.meta.url).href, { type: "module" });
        const timer = setTimeout(() => {
            worker.terminate();
            reject(new Error(`upload scenario timed out: ${scenario}`));
        }, 4000);
        const finish = () => {
            clearTimeout(timer);
            worker.terminate();
        };
        worker.onmessage = (event) => {
            finish();
            if (event.data.ok) resolve(event.data.result);
            else reject(new Error(event.data.error));
        };
        worker.onerror = (event) => {
            finish();
            reject(event.error ?? new Error(event.message));
        };
        worker.postMessage(scenario);
    });
}

test("video and poster requests overlap, and success waits for both", async () => {
    const result = await runScenario("success");
    expect(result.overlapped).toBe(true);
    expect(result.settledBeforePoster).toBe(false);
    expect(result.previewReleased).toBe(true);
    expect(result.storageKey).toBe("resource:video-result");
    expect(result.posterKey).toBe("resource:image-result");
    expect(result.error).toBeUndefined();
});

test("poster failure does not discard a remotely saved video", async () => {
    const result = await runScenario("poster-failure");
    expect(result.overlapped).toBe(true);
    expect(result.storageKey).toBe("resource:video-result");
    expect(result.posterKey).toBeUndefined();
    expect(result.pendingRemoteUpload).toBeUndefined();
    expect(result.previewReleased).toBe(true);
});

test("permanent video failure drains the poster and releases the preview", async () => {
    const result = await runScenario("video-failure");
    expect(result.overlapped).toBe(true);
    expect(result.settledBeforePoster).toBe(false);
    expect(result.permanent).toBe(true);
    expect(result.error).toBe("video denied");
    expect(result.previewReleased).toBe(true);
});

test("failed local fallback still drains the poster and releases the preview", async () => {
    const result = await runScenario("local-failure");
    expect(result.overlapped).toBe(true);
    expect(result.settledBeforePoster).toBe(false);
    expect(result.error).toBe("local storage failed");
    expect(result.previewReleased).toBe(true);
});
