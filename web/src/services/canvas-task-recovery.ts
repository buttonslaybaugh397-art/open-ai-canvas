import { generationTaskStillOwnsNode } from "@/lib/canvas/canvas-generation-task-sync";
import { getActiveUserScope } from "@/lib/user-scope";
import { subscribeGenerationTasks, type GenerationTask } from "@/services/api/task-center";
import type { CanvasNodeData } from "@/types/canvas";

// Keep failed video subscriptions alive even after the original generation
// promise rejected. Server snapshots, not events in the admin's browser, drive this.
export function observeCanvasTaskRecovery(input: {
    projectId: string;
    taskIds: string[];
    nodes: () => CanvasNodeData[];
    onUpdate: (nodeId: string, task: GenerationTask) => void;
    onSuccess: (nodeId: string, task: GenerationTask, signal: AbortSignal) => Promise<void>;
    onError: (error: unknown) => void;
    subscribe?: typeof subscribeGenerationTasks;
    getScope?: () => string;
    retryDelayMs?: number;
}) {
    const getScope = input.getScope ?? getActiveUserScope;
    const scope = getScope();
    const controller = new AbortController();
    const applying = new Set<string>();
    const retryTimers = new Set<ReturnType<typeof setTimeout>>();
    const current = () => !controller.signal.aborted && getScope() === scope;
    const observe = (task: GenerationTask) => {
        if (!current() || task.projectId !== input.projectId || !task.providerRecoveryAt) return;
        for (const node of input.nodes()) {
            if (!generationTaskStillOwnsNode(node, task)) continue;
            if (task.status !== "succeeded") {
                input.onUpdate(node.id, task);
                continue;
            }
            if (node.metadata?.status === "success" && node.metadata.content && node.metadata.taskStatus === "succeeded") continue;
            if (applying.has(node.id)) continue;
            applying.add(node.id);
            void input
                .onSuccess(node.id, task, controller.signal)
                .catch((error) => {
                    if (!current() || (error instanceof Error && error.name === "AbortError")) return;
                    input.onError(error);
                    // Successful subscriptions stop polling; retry a failed local media
                    // attachment explicitly without reissuing a provider request.
                    const timer = setTimeout(() => {
                        retryTimers.delete(timer);
                        observe(task);
                    }, input.retryDelayMs ?? 5000);
                    retryTimers.add(timer);
                })
                .finally(() => applying.delete(node.id));
        }
    };
    const unsubscribe = (input.subscribe ?? subscribeGenerationTasks)(input.taskIds, observe);
    return () => {
        controller.abort();
        unsubscribe();
        retryTimers.forEach(clearTimeout);
    };
}
