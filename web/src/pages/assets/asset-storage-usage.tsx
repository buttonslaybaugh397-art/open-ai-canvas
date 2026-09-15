import { useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { Tooltip } from "antd";
import { HardDrive, RefreshCw } from "lucide-react";

import { formatBytes } from "@/lib/image-utils";
import { getAccountFileStorageUsage } from "@/services/api/resources";
import { useUserStore } from "@/stores/use-user-store";

export const assetStorageUsageQueryKey = ["account-file-storage-usage"] as const;

export function AssetStorageUsage() {
    const userId = useUserStore((state) => state.user?.id || "");
    const forceRecount = useRef(false);
    const query = useQuery({
        queryKey: [...assetStorageUsageQueryKey, userId],
        queryFn: ({ signal }) => {
            const refresh = forceRecount.current;
            forceRecount.current = false;
            return getAccountFileStorageUsage({ signal, refresh });
        },
        enabled: Boolean(userId),
        staleTime: 30_000,
        retry: false,
        refetchOnMount: "always",
        refetchOnWindowFocus: true,
        refetchInterval: (state) => (state.state.data?.pendingDeletionBytes ? 5_000 : 60_000),
        refetchIntervalInBackground: false,
    });
    const usage = query.data;
    const percent = usage?.totalBytes ? Math.min(100, (usage.usedBytes / usage.totalBytes) * 100) : 0;
    const percentLabel = usage?.usedBytes && percent < 0.1 ? "<0.1%" : `${Math.round(percent * 10) / 10}%`;
    const full = Boolean(usage && usage.usedBytes >= usage.totalBytes);
    const checkedAt = usage ? new Date(usage.checkedAt).toLocaleString() : "";

    return (
        <section
            className={`assets-storage-usage${full ? " is-full" : ""}${usage?.usedBytes ? " has-usage" : ""}`}
            aria-label="账号文件容量"
            aria-busy={query.isFetching}
            title={`资源原件和会话附件的实际占用，含回收站及待清理文件${checkedAt ? `；核验时间：${checkedAt}` : ""}`}
        >
            <span className="assets-storage-usage-icon" aria-hidden="true">
                <HardDrive />
            </span>
            <span className="assets-storage-usage-title">账号容量</span>
            {usage ? (
                <>
                    <span className="assets-storage-usage-value">
                        {storageBytes(usage.usedBytes)} / {storageBytes(usage.totalBytes)}
                    </span>
                    <span
                        className="assets-storage-usage-track"
                        role="progressbar"
                        aria-label="账号文件容量使用进度"
                        aria-valuemin={0}
                        aria-valuemax={usage.totalBytes}
                        aria-valuenow={Math.min(usage.usedBytes, usage.totalBytes)}
                        aria-valuetext={`已使用 ${storageBytes(usage.usedBytes)}，总容量 ${storageBytes(usage.totalBytes)}`}
                    >
                        <span style={{ width: `${percent}%` }} />
                    </span>
                    <span className="assets-storage-usage-percent">{percentLabel}</span>
                </>
            ) : !query.isError ? (
                <span className="assets-storage-usage-status">正在统计已用容量…</span>
            ) : null}
            <Tooltip title="重新核算实际占用">
                <button
                    type="button"
                    className="assets-storage-usage-refresh"
                    aria-label="重新核算实际占用"
                    disabled={!userId || query.isFetching}
                    onClick={() => {
                        forceRecount.current = true;
                        void query.refetch();
                    }}
                >
                    <RefreshCw aria-hidden="true" />
                </button>
            </Tooltip>
            {query.isError ? (
                <span className="assets-storage-usage-status is-error" role="status">
                    {usage ? `更新失败，显示上次统计（${checkedAt}）` : "容量统计暂时不可用"}
                </span>
            ) : usage && usage.pendingDeletionBytes > 0 ? (
                <span className="assets-storage-usage-status is-pending" role="status">
                    待物理清理 {storageBytes(usage.pendingDeletionBytes)}
                </span>
            ) : null}
        </section>
    );
}

function storageBytes(value: number) {
    return formatBytes(value) || "0 B";
}
