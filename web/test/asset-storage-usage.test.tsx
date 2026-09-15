import { afterEach, describe, expect, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { readFileSync } from "node:fs";

import { AssetStorageUsage, assetStorageUsageQueryKey } from "@/pages/assets/asset-storage-usage";
import { getAccountFileStorageUsage, parseAccountFileStorageUsage } from "@/services/api/resources";
import { apiClient } from "@/services/api/request";

const usage = {
    usedBytes: 100,
    totalBytes: 1000,
    resourceBytes: 60,
    sessionBytes: 10,
    pendingDeletionBytes: 30,
    checkedAt: "2026-09-15T01:02:03Z",
};
const originalAdapter = apiClient.defaults.adapter;
afterEach(() => {
    apiClient.defaults.adapter = originalAdapter;
});

describe("actual account file storage", () => {
    test("accepts measured values, zero and over-quota results", () => {
        expect(parseAccountFileStorageUsage(usage)).toEqual(usage);
        expect(parseAccountFileStorageUsage({ ...usage, usedBytes: 0, resourceBytes: 0, sessionBytes: 0, pendingDeletionBytes: 0 }).usedBytes).toBe(0);
        expect(parseAccountFileStorageUsage({ ...usage, totalBytes: 1 }).usedBytes).toBe(100);
    });
    test("rejects partial, invalid and inconsistent totals", () => {
        for (const invalid of [
            null,
            {},
            { usedBytes: 0, totalBytes: 100 },
            { ...usage, usedBytes: -1 },
            { ...usage, totalBytes: 0 },
            { ...usage, resourceBytes: 61 },
            { ...usage, sessionBytes: NaN },
            { ...usage, pendingDeletionBytes: 0.5 },
            { ...usage, checkedAt: "invalid" },
            { ...usage, usedBytes: Number.MAX_SAFE_INTEGER + 1 },
        ])
            expect(() => parseAccountFileStorageUsage(invalid)).toThrow("容量统计返回无效数据");
    });
    test("forwards recount and cancellation through the common API client", async () => {
        const controller = new AbortController();
        apiClient.defaults.adapter = async (config) => {
            expect(config.url).toBe("/resources/storage-usage");
            expect(config.params).toEqual({ refresh: 1 });
            expect(config.signal).toBe(controller.signal);
            expect(config.withCredentials).toBe(true);
            return { status: 200, statusText: "OK", headers: {}, config, data: { code: 0, data: { usage }, msg: "ok" } };
        };
        expect(await getAccountFileStorageUsage({ signal: controller.signal, refresh: true })).toEqual(usage);
        controller.abort();
        await expect(getAccountFileStorageUsage({ signal: controller.signal })).rejects.toMatchObject({ name: "AbortError" });
    });
    test("shows pending physical deletion and retains an explicit error on cached data", () => {
        const client = new QueryClient();
        const key = [...assetStorageUsageQueryKey, ""];
        client.setQueryData(key, usage);
        const render = () =>
            renderToStaticMarkup(
                <QueryClientProvider client={client}>
                    <AssetStorageUsage />
                </QueryClientProvider>,
            );
        expect(render()).toContain("待物理清理");
        expect(render()).toContain("重新核算实际占用");
        const query = client.getQueryCache().find({ queryKey: key, exact: true })!;
        query.setState({ status: "error", error: new Error("object store unavailable") });
        expect(render()).toContain("更新失败，显示上次统计");
        client.clear();
    });
    test("other account cache is not shown before login", () => {
        const client = new QueryClient();
        client.setQueryData([...assetStorageUsageQueryKey, "other-account"], usage);
        const html = renderToStaticMarkup(
            <QueryClientProvider client={client}>
                <AssetStorageUsage />
            </QueryClientProvider>,
        );
        expect(html).not.toContain("待物理清理");
        expect(html).toContain("正在统计已用容量");
        client.clear();
    });
    test("all destructive asset paths refresh even after partial failure", () => {
        const source = readFileSync(new URL("../src/pages/assets/index.tsx", import.meta.url), "utf8");
        for (const name of ["emptyTrash", "confirmDelete", "confirmBatchDelete"]) {
            const start = source.indexOf(`const ${name} = async`);
            const next = source.indexOf("\n    };", start);
            expect(source.slice(start, next)).toContain("finally {\n            await invalidateAssetLibrary();");
        }
        expect(source).toContain("queryClient.invalidateQueries({ queryKey: assetStorageUsageQueryKey })");
    });
});
