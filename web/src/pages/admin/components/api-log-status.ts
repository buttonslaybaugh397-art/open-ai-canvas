import type { ApiCallLog } from "@/services/api/auth";

export type ApiLogDisplayStatus = {
    label: "失败" | "处理中" | "成功" | "待确认";
    tone: "error" | "warning" | "success" | "neutral";
};

const processingStatuses = new Set(["1", "2", "queued", "queueing", "pending", "processing", "running", "generating", "in_progress"]);
const failedStatuses = new Set(["4", "fail", "failed", "failure", "error", "errored", "rejected", "cancelled", "canceled", "expired", "timeout", "timed_out"]);
const succeededStatuses = new Set(["3", "succeeded", "success", "completed", "complete", "done"]);

export function apiLogDisplayStatus(log: ApiCallLog): ApiLogDisplayStatus {
    const providerStatus = log.providerStatus?.trim().toLowerCase() || "";
    if (log.status === "failed" || log.statusCode >= 400 || Boolean(log.error || log.errorCode) || log.taskStatus === "failed" || log.taskStatus === "cancelled" || failedStatuses.has(providerStatus)) {
        return { label: "失败", tone: "error" };
    }
    if (log.taskStatus === "queued" || log.taskStatus === "running" || processingStatuses.has(providerStatus)) {
        return { label: "处理中", tone: "warning" };
    }
    if (log.taskStatus === "succeeded" || succeededStatuses.has(providerStatus) || (!providerStatus && log.status === "succeeded")) {
        return { label: "成功", tone: "success" };
    }
    return { label: "待确认", tone: "neutral" };
}
