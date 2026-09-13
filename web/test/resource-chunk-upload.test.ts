import { afterEach, expect, test } from "bun:test";
import { apiClient, ApiError } from "../src/services/api/request";
import { uploadResourceFile } from "../src/services/api/resources";

const previousAdapter = apiClient.defaults.adapter;
const size = 51 * 1024 * 1024;
const chunkSize = 8 * 1024 * 1024;
const chunkCount = Math.ceil(size / chunkSize);
const file = new Blob([new Uint8Array(size)]);
const session = { uploadId: "upload-session", chunkSize, chunkCount };

afterEach(() => {
    apiClient.defaults.adapter = previousAdapter;
});

test("chunk uploads use three slots and complete only after every chunk is acknowledged", async () => {
    let active = 0;
    let peak = 0;
    const completed = new Set<number>();
    const releases: Array<() => void> = [];
    let notifyStarted!: () => void;
    const started = new Promise<void>((resolve) => {
        notifyStarted = resolve;
    });
    apiClient.defaults.adapter = async (config) => {
        let data: unknown;
        if (config.url === "/resources/uploads") {
            expect(config.headers.get("X-Idempotency-Key")).toBe("stable-upload-key");
            data = session;
        } else if (config.url?.endsWith("/complete")) {
            expect(completed.size).toBe(chunkCount);
            expect(active).toBe(0);
            data = { resource: { id: "complete" } };
        } else {
            const index = Number(config.url?.split("/").at(-1));
            active++;
            peak = Math.max(peak, active);
            if (index < 3)
                await new Promise<void>((resolve) => {
                    releases.push(resolve);
                    if (releases.length === 3) notifyStarted();
                });
            completed.add(index);
            active--;
            data = { index };
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data } };
    };
    const pending = uploadResourceFile(file, "video", { idempotencyKey: "stable-upload-key" });
    await started;
    expect(active).toBe(3);
    releases.forEach((release) => release());
    expect((await pending).id).toBe("complete");
    expect(peak).toBe(3);
});

test("a transient chunk failure retries only that chunk within the original session", async () => {
    let starts = 0;
    let completions = 0;
    const attempts = new Map<number, number>();
    const progress: number[] = [];
    apiClient.defaults.adapter = async (config) => {
        let data: unknown;
        if (config.url === "/resources/uploads") {
            starts++;
            data = session;
        } else if (config.url?.endsWith("/complete")) {
            completions++;
            data = { resource: { id: "retried" } };
        } else {
            const index = Number(config.url?.split("/").at(-1));
            const attempt = (attempts.get(index) || 0) + 1;
            attempts.set(index, attempt);
            if (index === 1 && attempt === 1) throw new ApiError("try again", { status: 503, retryAfterMs: 0 });
            data = { index };
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data } };
    };
    await uploadResourceFile(file, "video", undefined, (loaded) => progress.push(loaded));
    expect(starts).toBe(1);
    expect(completions).toBe(1);
    expect(attempts.get(1)).toBe(2);
    expect([...attempts.entries()].filter(([index]) => index !== 1).every(([, count]) => count === 1)).toBe(true);
    expect(progress.at(-1)).toBe(size);
    expect(progress.every((value, index) => value >= (progress[index - 1] || 0) && value <= size)).toBe(true);
});

test("permanent chunk errors stop scheduling and never restart the whole upload", async () => {
    for (const status of [401, 403, 413]) {
        let starts = 0;
        const indices: number[] = [];
        apiClient.defaults.adapter = async (config) => {
            if (config.url === "/resources/uploads") {
                starts++;
                return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: session } };
            }
            expect(config.url).not.toContain("/complete");
            indices.push(Number(config.url?.split("/").at(-1)));
            throw new ApiError("not allowed", { status });
        };
        await expect(uploadResourceFile(file, "video")).rejects.toMatchObject({ permanent: true, status });
        expect(starts).toBe(1);
        expect(indices.length).toBeLessThanOrEqual(3);
        expect(new Set(indices).size).toBe(indices.length);
    }
});

test("exhausted transient retries propagate failure without resending successful chunks", async () => {
    const attempts = new Map<number, number>();
    apiClient.defaults.adapter = async (config) => {
        if (config.url === "/resources/uploads") return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: session } };
        expect(config.url).not.toContain("/complete");
        const index = Number(config.url?.split("/").at(-1));
        attempts.set(index, (attempts.get(index) || 0) + 1);
        if (index === 1) throw new ApiError("unavailable", { status: 503, retryAfterMs: 0 });
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: { index } } };
    };
    await expect(uploadResourceFile(file, "video")).rejects.toMatchObject({ permanent: false });
    expect(attempts.get(1)).toBe(3);
    expect([...attempts.entries()].filter(([index]) => index !== 1).every(([, count]) => count === 1)).toBe(true);
});

test("a failed completion does not automatically reupload the file", async () => {
    let starts = 0;
    let chunks = 0;
    let completions = 0;
    apiClient.defaults.adapter = async (config) => {
        let data: unknown;
        if (config.url === "/resources/uploads") {
            starts++;
            data = session;
        } else if (config.url?.endsWith("/complete")) {
            completions++;
            throw new ApiError("storage failed", { status: 503 });
        } else {
            chunks++;
            data = { index: 0 };
        }
        return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data } };
    };
    await expect(uploadResourceFile(file, "video")).rejects.toThrow("storage failed");
    expect([starts, chunks, completions]).toEqual([1, chunkCount, 1]);
});

test("cancellation stays an AbortError and invalid session metadata never starts chunks", async () => {
    for (const response of ["abort", null, { ...session, uploadId: 123 }, { ...session, chunkCount: 0 }, { ...session, chunkSize: 0 }]) {
        apiClient.defaults.adapter = async (config) => {
            if (response === "abort") throw new DOMException("cancelled", "AbortError");
            expect(config.url).toBe("/resources/uploads");
            return { config, status: 200, statusText: "OK", headers: {}, data: { code: 0, data: response } };
        };
        if (response === "abort") await expect(uploadResourceFile(file, "video")).rejects.toMatchObject({ name: "AbortError" });
        else await expect(uploadResourceFile(file, "video")).rejects.toMatchObject({ permanent: true });
    }
});
