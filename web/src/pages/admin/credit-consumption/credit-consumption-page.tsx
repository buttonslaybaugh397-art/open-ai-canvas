import { App, Button, DatePicker, Input, Select, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs, { type Dayjs } from "dayjs";
import { BarChart3, RotateCcw } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Area, CartesianGrid, ComposedChart, ResponsiveContainer, Tooltip as ChartTooltip, XAxis, YAxis } from "recharts";
import { useSearchParams } from "react-router";

import { formatCredits } from "@/constant/credits";
import { AdminDataTable, AdminStatTile, AdminStatusBadge, AdminTableEmpty } from "@/pages/admin/components/admin-ui";
import { AdminPageFrame } from "@/pages/admin/components/admin-shell";
import { getAdminCreditConsumption, type AdminCreditConsumptionOverview } from "@/services/api/wallet";

const { RangePicker } = DatePicker;
const pageSize = 20;

const capabilityOptions = [
    { label: "文本", value: "text" },
    { label: "图片", value: "image" },
    { label: "视频", value: "video" },
    { label: "音频", value: "audio" },
];

export default function CreditConsumptionPage() {
    const { message } = App.useApp();
    const [searchParams, setSearchParams] = useSearchParams();
    const [range, setRange] = useState<[Dayjs, Dayjs]>(() => [readDate(searchParams.get("from"), dayjs().subtract(29, "day")), readDate(searchParams.get("to"), dayjs())]);
    const [userId, setUserId] = useState(searchParams.get("userId") || "");
    const [model, setModel] = useState(searchParams.get("model") || "");
    const [capability, setCapability] = useState(searchParams.get("capability") || "");
    const [userPage, setUserPage] = useState(1);
    const [modelPage, setModelPage] = useState(1);
    const [data, setData] = useState<AdminCreditConsumptionOverview | null>(null);
    const [loading, setLoading] = useState(false);

    const filters = useMemo(
        () => ({
            from: range[0].format("YYYY-MM-DD"),
            to: range[1].format("YYYY-MM-DD"),
            userId: userId || undefined,
            model: model || undefined,
            capability: capability || undefined,
            userPage,
            userLimit: pageSize,
            modelPage,
            modelLimit: pageSize,
        }),
        [capability, model, modelPage, range, userId, userPage],
    );

    const reload = useCallback(async () => {
        setLoading(true);
        try {
            setData(await getAdminCreditConsumption(filters));
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取积分消耗统计失败");
        } finally {
            setLoading(false);
        }
    }, [filters, message]);

    useEffect(() => {
        const next = new URLSearchParams();
        next.set("from", filters.from);
        next.set("to", filters.to);
        if (filters.userId) next.set("userId", filters.userId);
        if (filters.model) next.set("model", filters.model);
        if (filters.capability) next.set("capability", filters.capability);
        setSearchParams(next, { replace: true });
        void reload();
    }, [filters, reload, setSearchParams]);

    const resetFilters = () => {
        setRange([dayjs().subtract(29, "day"), dayjs()]);
        setUserId("");
        setModel("");
        setCapability("");
        setUserPage(1);
        setModelPage(1);
    };

    const drillIntoUser = (id: string) => {
        setUserId(id);
        setUserPage(1);
        setModelPage(1);
    };

    const drillIntoModel = (name: string, modelCapability: string) => {
        setModel(name);
        setCapability(modelCapability);
        setUserPage(1);
        setModelPage(1);
    };

    const userColumns: ColumnsType<AdminCreditConsumptionOverview["users"]["items"][number]> = [
        {
            title: "用户",
            dataIndex: "username",
            width: 220,
            render: (username, row) => (
                <div>
                    <div className="font-medium">{row.displayName || username || row.userId}</div>
                    <div className="mt-0.5 text-xs text-foreground/45">{username ? `@${username}` : row.userId}</div>
                </div>
            ),
        },
        { title: "区间消耗", dataIndex: "totalMicrocredits", width: 150, align: "right", render: (value) => <span className="font-medium tabular-nums">{formatCredits(value, 6)}</span> },
        { title: "结算任务", dataIndex: "orderCount", width: 110, align: "right" },
        { title: "使用模型", dataIndex: "modelCount", width: 110, align: "right" },
        { title: "最近消耗", dataIndex: "lastConsumedAt", width: 170, render: formatDateTime },
        {
            title: "操作",
            key: "actions",
            fixed: "right",
            width: 110,
            render: (_, row) => (
                <Button type="link" size="small" onClick={() => drillIntoUser(row.userId)}>
                    单用户统计
                </Button>
            ),
        },
    ];

    const modelColumns: ColumnsType<AdminCreditConsumptionOverview["models"]["items"][number]> = [
        {
            title: "模型",
            dataIndex: "model",
            width: 260,
            render: (value, row) => (
                <div>
                    <div className="font-medium">{value || "未识别模型"}</div>
                    <div className="mt-1"><AdminStatusBadge label={capabilityLabel(row.capability)} tone="info" /></div>
                </div>
            ),
        },
        { title: "区间消耗", dataIndex: "totalMicrocredits", width: 150, align: "right", render: (value) => <span className="font-medium tabular-nums">{formatCredits(value, 6)}</span> },
        { title: "结算任务", dataIndex: "orderCount", width: 110, align: "right" },
        { title: "使用用户", dataIndex: "uniqueUsers", width: 110, align: "right" },
        { title: "最近消耗", dataIndex: "lastConsumedAt", width: 170, render: formatDateTime },
        {
            title: "操作",
            key: "actions",
            fixed: "right",
            width: 100,
            render: (_, row) => (
                <Button type="link" size="small" onClick={() => drillIntoModel(row.model, row.capability)}>
                    查看明细
                </Button>
            ),
        },
    ];

    const trend = useMemo(() => (data?.trend || []).map((item) => ({ ...item, credits: item.totalMicrocredits / 1_000_000 })), [data?.trend]);
    const summary = data?.summary;
    const filtered = Boolean(userId || model || capability);

    return (
        <AdminPageFrame
            title="积分消耗统计"
            description="按已结算账单统计全站、用户和模型的实际积分消耗"
            scroll
            actions={filtered ? <Button icon={<RotateCcw className="size-4" />} onClick={resetFilters}>清除下钻</Button> : undefined}
        >
            <div className="space-y-4 py-4">
                <section className="rounded-lg border border-border bg-[var(--workspace-surface)] p-4">
                    <div className="flex flex-wrap items-end gap-3">
                        <div>
                            <div className="mb-1 text-xs text-foreground/55">统计日期</div>
                            <RangePicker
                                value={range}
                                allowClear={false}
                                onChange={(value) => {
                                    if (!value?.[0] || !value[1]) return;
                                    setRange([value[0], value[1]]);
                                    setUserPage(1);
                                    setModelPage(1);
                                }}
                            />
                        </div>
                        <div>
                            <div className="mb-1 text-xs text-foreground/55">能力类型</div>
                            <Select allowClear placeholder="全部能力" value={capability || undefined} options={capabilityOptions} style={{ width: 150 }} onChange={(value) => { setCapability(value || ""); setUserPage(1); setModelPage(1); }} />
                        </div>
                        <div>
                            <div className="mb-1 text-xs text-foreground/55">模型标识</div>
                            <Input allowClear placeholder="筛选模型" value={model} style={{ width: 190 }} onChange={(event) => { setModel(event.target.value); setUserPage(1); setModelPage(1); }} />
                        </div>
                        <Button onClick={() => void reload()} loading={loading}>刷新统计</Button>
                    </div>
                    {filtered ? (
                        <div className="mt-3 flex flex-wrap gap-2 border-t border-border/70 pt-3">
                            {userId ? <Tag closable onClose={() => setUserId("")}>用户：{selectedUserLabel(data, userId)}</Tag> : null}
                            {model ? <Tag closable onClose={() => setModel("")}>模型：{model}</Tag> : null}
                            {capability ? <Tag closable onClose={() => setCapability("")}>能力：{capabilityLabel(capability)}</Tag> : null}
                        </div>
                    ) : null}
                </section>

                <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
                    <AdminStatTile label={filtered ? "当前筛选累计消耗" : "全站累计消耗"} value={summary ? formatCredits(summary.allTimeMicrocredits, 6) : "--"} detail="全部历史已结算账单" />
                    <AdminStatTile label="所选区间消耗" value={summary ? formatCredits(summary.periodMicrocredits, 6) : "--"} detail={`${filters.from} 至 ${filters.to}`} />
                    <AdminStatTile label="结算任务" value={summary?.settledOrders ?? "--"} detail="不含预授权与退款" />
                    <AdminStatTile label="消耗用户" value={summary?.consumingUsers ?? "--"} detail={userId ? "当前单用户" : "区间去重用户"} />
                    <AdminStatTile label="使用模型" value={summary?.usedModels ?? "--"} detail="区间去重模型" />
                </div>

                <section className="rounded-lg border border-border bg-[var(--workspace-surface)] p-4">
                    <div className="mb-3">
                        <h2 className="text-sm font-semibold">每日消耗趋势</h2>
                        <p className="mt-1 text-xs text-foreground/50">按北京时间归属结算日期，金额单位为积分。</p>
                    </div>
                    {trend.length ? (
                        <div className="h-[280px] w-full">
                            <ResponsiveContainer width="100%" height="100%">
                                <ComposedChart data={trend} margin={{ top: 8, right: 12, bottom: 0, left: 4 }}>
                                    <CartesianGrid stroke="currentColor" className="text-foreground/10" vertical={false} />
                                    <XAxis dataKey="day" tickFormatter={(value) => String(value).slice(5)} tick={{ fontSize: 11 }} />
                                    <YAxis tick={{ fontSize: 11 }} />
                                    <ChartTooltip labelFormatter={(value) => `日期 ${value}`} formatter={(value) => [`${Number(value).toLocaleString("zh-CN", { maximumFractionDigits: 6 })} 积分`, "消耗"]} />
                                    <Area type="monotone" dataKey="credits" name="积分消耗" stroke="var(--admin-chart-primary)" fill="var(--admin-chart-primary)" fillOpacity={0.12} />
                                </ComposedChart>
                            </ResponsiveContainer>
                        </div>
                    ) : (
                        <div className="flex min-h-56 flex-col items-center justify-center text-center text-foreground/50">
                            <BarChart3 className="mb-3 size-6" />
                            <div className="text-sm font-medium text-foreground/70">当前范围暂无已结算消耗</div>
                            <div className="mt-1 text-xs">调整日期或清除筛选后再查看。</div>
                        </div>
                    )}
                </section>

                <section>
                    <div className="mb-2">
                        <h2 className="text-sm font-semibold">用户消耗</h2>
                        <p className="mt-1 text-xs text-foreground/50">点击“单用户统计”后，顶部累计与模型表都会切换到该用户。</p>
                    </div>
                    <AdminDataTable
                        table={{
                            rowKey: "userId",
                            size: "small",
                            loading,
                            columns: userColumns,
                            dataSource: data?.users.items || [],
                            scroll: { x: 900 },
                            pagination: { current: userPage, pageSize, total: data?.users.total || 0, showSizeChanger: false, onChange: setUserPage },
                        }}
                        empty={<AdminTableEmpty title="当前范围暂无用户消耗" />}
                        skeletonColumns={6}
                    />
                </section>

                <section>
                    <div className="mb-2">
                        <h2 className="text-sm font-semibold">模型消耗</h2>
                        <p className="mt-1 text-xs text-foreground/50">按模型与能力类型分组，单用户下钻后展示该用户分别消耗在哪些模型。</p>
                    </div>
                    <AdminDataTable
                        table={{
                            rowKey: (row) => `${row.model}:${row.capability}`,
                            size: "small",
                            loading,
                            columns: modelColumns,
                            dataSource: data?.models.items || [],
                            scroll: { x: 900 },
                            pagination: { current: modelPage, pageSize, total: data?.models.total || 0, showSizeChanger: false, onChange: setModelPage },
                        }}
                        empty={<AdminTableEmpty title="当前范围暂无模型消耗" />}
                        skeletonColumns={6}
                    />
                </section>
            </div>
        </AdminPageFrame>
    );
}

function readDate(value: string | null, fallback: Dayjs) {
    const parsed = dayjs(value || "");
    return parsed.isValid() ? parsed : fallback;
}

function capabilityLabel(value: string) {
    return capabilityOptions.find((item) => item.value === value)?.label || value || "未分类";
}

function formatDateTime(value: string) {
    const parsed = dayjs(value);
    return parsed.isValid() ? parsed.format("YYYY-MM-DD HH:mm") : "--";
}

function selectedUserLabel(data: AdminCreditConsumptionOverview | null, userId: string) {
    const user = data?.users.items.find((item) => item.userId === userId);
    return user?.displayName || user?.username || userId;
}
