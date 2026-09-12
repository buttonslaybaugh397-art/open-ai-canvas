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
