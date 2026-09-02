import { apiLogDisplayStatus } from "../src/pages/admin/components/api-log-status";
import type { ApiCallLog } from "../src/services/api/auth";

function log(patch: Partial<ApiCallLog>): ApiCallLog {
    return { status: "succeeded", statusCode: 200, providerStatus: "", taskStatus: undefined, error: "", errorCode: "", ...patch } as ApiCallLog;
}

test("API log status never presents failures or unknown provider states as success", () => {
    expect(apiLogDisplayStatus(log({ providerStatus: "processing" }))).toEqual({ label: "处理中", tone: "warning" });
    expect(apiLogDisplayStatus(log({ providerStatus: "4" }))).toEqual({ label: "失败", tone: "error" });
    expect(apiLogDisplayStatus(log({ taskStatus: "failed" }))).toEqual({ label: "失败", tone: "error" });
    expect(apiLogDisplayStatus(log({ providerStatus: "vendor_custom_state" }))).toEqual({ label: "待确认", tone: "neutral" });
    expect(apiLogDisplayStatus(log({ providerStatus: "completed" }))).toEqual({ label: "成功", tone: "success" });
});
