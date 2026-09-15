import { expect, test } from "bun:test";
import { createGenerationTaskSubscriptionService, generationTaskSnapshotCanAdvance, type GenerationTask } from "../src/services/api/task-center";

function task(status: GenerationTask["status"] = "running"): GenerationTask {
    return {
        id: "stable-task",
        type: "canvas_video",
        status,
        prompt: "fixture",
        attempts: 1,
        createdAt: "2026-09-01T00:00:00Z",
        updatedAt: "2026-09-01T00:01:00Z",
    };
}

test("shared task snapshots never regress from newer progress or a terminal state", () => {
    const running = { ...task(), progress: 60, updatedAt: "2026-09-01T00:02:00Z" };
    const succeeded = { ...running, status: "succeeded" as const, progress: 100, updatedAt: "2026-09-01T00:03:00Z" };

    expect(generationTaskSnapshotCanAdvance(running, { ...running, progress: 20 })).toBe(false);
    expect(generationTaskSnapshotCanAdvance(running, { ...running, progress: 80 })).toBe(true);
    expect(generationTaskSnapshotCanAdvance(running, { ...running, status: "queued", progress: 80 })).toBe(false);
    expect(generationTaskSnapshotCanAdvance(succeeded, { ...running, progress: 90, updatedAt: "2026-09-01T00:04:00Z" })).toBe(false);
    expect(generationTaskSnapshotCanAdvance({ ...succeeded, status: "failed" }, { ...succeeded, updatedAt: "2026-09-01T00:01:00Z" })).toBe(true);
});

test("shared task observation retries a read failure and ignores a late running snapshot", async () => {
    const running = { ...task(), progress: 40 };
    const succeeded = { ...running, status: "succeeded" as const, progress: 100, updatedAt: "2026-09-01T00:02:00Z" };
    let queries = 0;
    const service = createGenerationTaskSubscriptionService({
        retryDelayMs: 0,
        queryTask: async () => {
            queries += 1;
            if (queries === 1) throw new Error("temporary read failure");
            return running;
        },
        waitTask: async (_id, options) => {
            options?.onTaskUpdate?.(succeeded);
            options?.onTaskUpdate?.({ ...running, progress: 90, updatedAt: "2026-09-01T00:03:00Z" });
            return succeeded;
        },
    });
    const observed: GenerationTask[] = [];
    const unsubscribe = service.subscribe([running.id], (snapshot) => observed.push(snapshot));

    try {
        await new Promise((resolve) => setTimeout(resolve, 20));
        expect(queries).toBe(2);
        expect(observed.map((snapshot) => snapshot.status)).toEqual(["running", "succeeded"]);
    } finally {
        unsubscribe();
    }
});

test("only a newer recovery epoch reopens failure and fences stale terminal snapshots", () => {
    const failed = { ...task("failed"), providerRequestId: "original" };
    const resumed = { ...failed, status: "running" as const, providerRecoveryAt: "2026-09-01T00:02:00Z", updatedAt: "2026-09-01T00:02:00Z" };
    expect(generationTaskSnapshotCanAdvance(failed, resumed)).toBe(true);
    expect(generationTaskSnapshotCanAdvance(resumed, failed)).toBe(false);
    expect(generationTaskSnapshotCanAdvance(resumed, { ...failed, status: "succeeded" })).toBe(false);
    expect(generationTaskSnapshotCanAdvance(failed, { ...resumed, providerRecoveryAt: undefined })).toBe(false);
    expect(generationTaskSnapshotCanAdvance({ ...failed, status: "cancelled" }, resumed)).toBe(false);
    expect(generationTaskSnapshotCanAdvance(resumed, { ...resumed, status: "succeeded", progress: 100 })).toBe(true);
});

test("failed video observation discovers admin recovery and resumes polling the same task", async () => {
    const failed = { ...task("failed"), providerRequestId: "original" };
    const resumed = { ...failed, status: "running" as const, providerRecoveryAt: "2026-09-01T00:02:00Z", updatedAt: "2026-09-01T00:02:00Z" };
    const completed = { ...resumed, status: "succeeded" as const, progress: 100, updatedAt: "2026-09-01T00:03:00Z" };
    let queries = 0;
    let waits = 0;
    const service = createGenerationTaskSubscriptionService({
        recoveryPollMs: 1,
        queryTask: async () => (++queries < 3 ? failed : resumed),
        waitTask: async (id, options) => {
            expect(id).toBe(failed.id);
            waits++;
            options?.onTaskUpdate?.(failed);
            return completed;
        },
    });
    const observed: string[] = [];
    const done = Promise.withResolvers<void>();
    const unsubscribe = service.subscribe([failed.id], (snapshot) => {
        observed.push(snapshot.status);
        if (snapshot.status === "succeeded") done.resolve();
    });
    try {
        await done.promise;
        expect(observed).toEqual(["failed", "running", "succeeded"]);
        expect(waits).toBe(1);
        const count = queries;
        await new Promise((resolve) => setTimeout(resolve, 10));
        expect(queries).toBe(count);
    } finally {
        unsubscribe();
    }
});

test("account switch fences old responses and starts a separate observation", async () => {
    let scope = "a";
    const pending = Promise.withResolvers<GenerationTask>();
    const service = createGenerationTaskSubscriptionService({
        getScope: () => scope,
        queryTask: async () => (scope === "a" ? pending.promise : { ...task("succeeded") }),
        waitTask: async () => {
            throw new Error("unexpected wait");
        },
    });
    const old: GenerationTask[] = [];
    const next: GenerationTask[] = [];
    const stopOld = service.subscribe(["stable-task"], (task) => old.push(task));
    scope = "b";
    const stopNew = service.subscribe(["stable-task"], (task) => next.push(task));
    pending.resolve(task("failed"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    stopOld();
    stopNew();
    expect(old).toEqual([]);
    expect(next.map((task) => task.status)).toEqual(["succeeded"]);
});
