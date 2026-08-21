import { parseBackendGenerationResult } from "@/services/api/generation-task";
import type { GenerationTask } from "@/services/api/task-center";

export type RecoverableCreationTextMessage = {
    mode?: string;
    status?: string;
    content: string;
    error?: string;
    taskIds?: string[];
};

export type CreationTextTaskRecovery = {
    status: "done" | "error" | "cancelled";
    content: string;
    error?: string;
    taskIds: string[];
};

// 页面重载后只依赖后端任务终态恢复消息，不复用已经中断的浏览器请求。
export function recoverCreationTextTask(message: RecoverableCreationTextMessage, tasks: GenerationTask[]): CreationTextTaskRecovery | null {
    if (message.mode !== "text" || (message.status !== "streaming" && message.status !== "pending")) return null;

    const taskIds = new Set(message.taskIds || []);
    const matches = tasks.filter((task) => taskIds.has(task.id));
    if (!matches.length || matches.some((task) => task.status === "queued" || task.status === "running")) return null;

    const nextTaskIds = Array.from(new Set([...(message.taskIds || []), ...matches.map((task) => task.id)]));
    const succeeded = matches.find((task) => task.status === "succeeded");
    if (succeeded) {
        try {
            const text = parseBackendGenerationResult(succeeded).text;
            if (!text?.trim()) throw new Error("后端任务没有返回文本");
            return { status: "done", content: text, error: undefined, taskIds: nextTaskIds };
        } catch (error) {
            return { status: "error", content: "生成失败", error: error instanceof Error ? error.message : "文本任务结果格式错误", taskIds: nextTaskIds };
        }
    }
    // text_replay 是前端自管状态，worker 不会接管它：浏览器在生成中途关闭后任务会永久停在这里。
    // 此时后端仍存着已上报的草稿，交还给用户并说明生成被中断，不能谎报“生成失败”。
    const interrupted = matches.find((task) => task.status === "text_replay");
    if (interrupted) {
        const draft = interrupted.textDraft?.trim();
        if (draft) return { status: "error", content: draft, error: "生成在上次会话中被中断，仅恢复已保存的部分正文", taskIds: nextTaskIds };
        return { status: "error", content: "生成已中断", error: "生成在上次会话中被中断，没有保存到正文", taskIds: nextTaskIds };
    }
    if (matches.every((task) => task.status === "cancelled")) {
        return { status: "cancelled", content: "已停止", error: undefined, taskIds: nextTaskIds };
    }
    const failed = matches.find((task) => task.status === "failed");
    return { status: "error", content: "生成失败", error: failed?.error || "文本任务已失败", taskIds: nextTaskIds };
}
