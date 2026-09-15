import { expect, test } from "bun:test";
import { generationTaskStillOwnsNode, mergeGenerationTaskResultNodes } from "../src/lib/canvas/canvas-generation-task-sync";
import { observeCanvasTaskRecovery } from "../src/services/canvas-task-recovery";
import type { GenerationTask } from "../src/services/api/task-center";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

const recovered: GenerationTask = {
    id: "task",
    projectId: "canvas",
    type: "canvas_video",
    status: "running",
    prompt: "test",
    attempts: 1,
    providerRequestId: "original",
    providerRecoveryAt: "2026-09-01T00:02:00Z",
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:02:00Z",
};
function failedNode(): CanvasNodeData {
    return {
        id: "node",
        type: CanvasNodeType.Video,
        title: "Video",
        position: { x: 0, y: 0 },
        width: 200,
        height: 100,
        metadata: { taskId: "task", taskStatus: "failed", taskUpdatedAt: "2026-09-01T00:01:00Z", status: "error" },
    };
}

test("cross-client recovery updates the live bound node without refreshing the page", async () => {
    let nodes = [failedNode()];
    let listener!: (task: GenerationTask) => void;
    let successes = 0;
    const stop = observeCanvasTaskRecovery({
        projectId: "canvas",
        taskIds: ["task"],
        nodes: () => nodes,
        getScope: () => "user",
        subscribe: (_ids, onTask) => {
            listener = onTask;
            return () => {};
        },
        onUpdate: (nodeId, task) => {
            nodes = nodes.map((node) => (node.id === nodeId ? { ...node, metadata: { ...node.metadata, taskStatus: task.status, status: "loading" } } : node));
        },
        onSuccess: async () => {
            successes++;
            nodes = nodes.map((node) => ({ ...node, metadata: { ...node.metadata, content: "/api/resources/video/file", status: "success", taskStatus: "succeeded" } }));
        },
        onError: (error) => {
            throw error;
        },
    });
    listener(recovered);
    expect(nodes[0]!.metadata?.status).toBe("loading");
    listener({ ...recovered, status: "succeeded" });
    listener({ ...recovered, status: "succeeded" });
    await Promise.resolve();
    expect(successes).toBe(1);
    expect(nodes[0]!.metadata?.content).toBe("/api/resources/video/file");
    stop();
});

test("recovery ignores another canvas, account switch, replaced and deleted nodes", async () => {
    let scope = "owner";
    let nodes = [failedNode()];
    let listener!: (task: GenerationTask) => void;
    let changes = 0;
    const stop = observeCanvasTaskRecovery({
        projectId: "canvas",
        taskIds: ["task"],
        nodes: () => nodes,
        getScope: () => scope,
        subscribe: (_ids, onTask) => {
            listener = onTask;
            return () => {};
        },
        onUpdate: () => {
            changes++;
        },
        onSuccess: async () => {
            changes++;
        },
        onError: (error) => {
            throw error;
        },
    });
    listener({ ...recovered, projectId: "other" });
    nodes = [{ ...failedNode(), metadata: { taskId: "new-task" } }];
    listener(recovered);
    nodes = [];
    listener(recovered);
    nodes = [failedNode()];
    scope = "other";
    listener({ ...recovered, status: "succeeded" });
    stop();
    expect(changes).toBe(0);
});

test("async media result preserves new edits and refuses replacement task/media", () => {
    const base = failedNode();
    const applied = { ...base, width: 300, metadata: { ...base.metadata, status: "success" as const, taskStatus: "succeeded", content: "video-result", taskProviderRecoveryAt: recovered.providerRecoveryAt } };
    const edited = { ...base, title: "Renamed", position: { x: 400, y: 300 }, metadata: { ...base.metadata, composerContent: "Next prompt" } };
    const merged = mergeGenerationTaskResultNodes([edited], [base], [applied], recovered, base.id);
    expect(merged[0]).toMatchObject({ title: "Renamed", position: { x: 400, y: 300 }, width: 300, metadata: { composerContent: "Next prompt", content: "video-result", status: "success" } });
    for (const metadata of [{ taskId: "new-task" }, { ...base.metadata, content: "manual-replacement" }, { ...base.metadata, taskProviderRecoveryAt: "2026-09-01T00:04:00Z" }]) {
        const current = [{ ...edited, metadata }];
        expect(mergeGenerationTaskResultNodes(current, [base], [applied], recovered, base.id)).toBe(current);
    }
    expect(generationTaskStillOwnsNode(undefined, recovered)).toBe(false);
    expect(mergeGenerationTaskResultNodes([], [base], [applied], recovered, base.id)).toEqual([]);
});
