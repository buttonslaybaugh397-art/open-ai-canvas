import { App, Button, DatePicker, Input, Select, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs, { type Dayjs } from "dayjs";
import { Activity, ArrowDownRight, ArrowUpRight, BarChart3, CalendarDays, CircleDollarSign, Clock3, Coins, Layers3, RefreshCw, RotateCcw, Sparkles, Target, UsersRound } from "lucide-react";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { Area, Bar, CartesianGrid, ComposedChart, Legend, ResponsiveContainer, Tooltip as ChartTooltip, XAxis, YAxis } from "recharts";
import { useSearchParams } from "react-router";

import { formatCredits } from "@/constant/credits";
import { AdminDataTable, AdminStatusBadge, AdminTableEmpty } from "@/pages/admin/components/admin-ui";
import { AdminPageFrame } from "@/pages/admin/components/admin-shell";
import { getAdminCreditConsumption, type AdminCreditConsumptionOverview } from "@/services/api/wallet";

const { RangePicker } = DatePicker;
const pageSize = 20;
const creditScale = 1_000_000;

const capabilityOptions = [
    { label: "文本", value: "text" },
    { label: "图片", value: "image" },
    { label: "视频", value: "video" },
    { label: "音频", value: "audio" },
];

const rangePresets = [
    { label: "近 7 天", days: 7 },
    { label: "近 30 天", days: 30 },
    { label: "近 90 天", days: 90 },
];

type ConsumptionUser = AdminCreditConsumptionOverview["users"]["items"][number];
type ConsumptionModel = AdminCreditConsumptionOverview["models"]["items"][number];

export default function CreditConsumptionPage() {
    const { message } = App.useApp();
    const [searchParams, setSearchParams] = useSearchParams();
    const [range, setRange] = useState<[Dayjs, Dayjs]>(() => [readDate(searchParams.get("from"), dayjs().subtract(29, "day")), readDate(searchParams.get("to"), dayjs())]);
    const [userId, setUserId] = useState(searchParams.get("userId") || "");
    const [model, setModel] = useState(searchParams.get("model") || "");
    const [modelDraft, setModelDraft] = useState(searchParams.get("model") || "");
    const [capability, setCapability] = useState(searchParams.get("capability") || "");
    const [userPage, setUserPage] = useState(1);
    const [modelPage, setModelPage] = useState(1);
    const [data, setData] = useState<AdminCreditConsumptionOverview | null>(null);
    const [loading, setLoading] = useState(false);
    const [updatedAt, setUpdatedAt] = useState<Dayjs | null>(null);

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
            setUpdatedAt(dayjs());
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

    const resetPages = () => {
        setUserPage(1);
        setModelPage(1);
    };

    const resetFilters = () => {
        setRange([dayjs().subtract(29, "day"), dayjs()]);
        setUserId("");
        setModel("");
        setModelDraft("");
        setCapability("");
        resetPages();
    };

    const selectRange = (days: number) => {
        setRange([dayjs().subtract(days - 1, "day"), dayjs()]);
        resetPages();
    };

    const applyModelFilter = () => {
        setModel(modelDraft.trim());
        resetPages();
    };

    const drillIntoUser = (id: string) => {
        setUserId(id);
        resetPages();
    };

    const drillIntoModel = (name: string, modelCapability: string) => {
        setModel(name);
        setModelDraft(name);
        setCapability(modelCapability);
        resetPages();
    };

    const summary = data?.summary;
    const periodTotal = summary?.periodMicrocredits || 0;
    const periodDays = Math.max(1, range[1].startOf("day").diff(range[0].startOf("day"), "day") + 1);
    const trend = useMemo(() => (data?.trend || []).map((item) => ({ ...item, credits: item.totalMicrocredits / creditScale })), [data?.trend]);
    const activeDays = trend.filter((item) => item.totalMicrocredits > 0).length;
    const peakDay = trend.reduce<(typeof trend)[number] | null>((peak, item) => item.totalMicrocredits <= 0 ? peak : (!peak || item.totalMicrocredits > peak.totalMicrocredits ? item : peak), null);
    const dailyAverage = safeDivide(periodTotal, periodDays);
    const orderAverage = safeDivide(periodTotal, summary?.settledOrders || 0);
    const userAverage = safeDivide(periodTotal, summary?.consumingUsers || 0);
    const filtered = Boolean(userId || model || capability);

    const userColumns: ColumnsType<ConsumptionUser> = [
        { title: "排名", width: 68, align: "center", render: (_, __, index) => <span className="admin-credit-rank">{(userPage - 1) * pageSize + index + 1}</span> },
        {
            title: "用户", dataIndex: "username", width: 230,
            render: (username, row) => <div className="admin-credit-entity"><strong>{row.displayName || username || row.userId}</strong><span>{row.email || (username ? `@${username}` : row.userId)}</span></div>,
        },
        { title: "区间消耗", dataIndex: "totalMicrocredits", width: 145, align: "right", render: (value) => <CreditValue value={value} /> },
        { title: "消耗占比", dataIndex: "totalMicrocredits", width: 150, render: (value) => <ConsumptionShare value={value} total={periodTotal} /> },
        { title: "结算任务", dataIndex: "orderCount", width: 100, align: "right", render: formatNumber },
        { title: "任务均耗", width: 120, align: "right", render: (_, row) => <span className="tabular-nums">{formatCredits(safeDivide(row.totalMicrocredits, row.orderCount), 6)}</span> },
        { title: "使用模型", dataIndex: "modelCount", width: 100, align: "right", render: formatNumber },
        { title: "最近消耗", dataIndex: "lastConsumedAt", width: 160, render: formatDateTime },
        { title: "操作", key: "actions", fixed: "right", width: 110, render: (_, row) => <Button type="link" size="small" onClick={() => drillIntoUser(row.userId)}>单用户统计</Button> },
    ];

    const modelColumns: ColumnsType<ConsumptionModel> = [
        { title: "排名", width: 68, align: "center", render: (_, __, index) => <span className="admin-credit-rank">{(modelPage - 1) * pageSize + index + 1}</span> },
        {
            title: "模型", dataIndex: "model", width: 260,
            render: (value, row) => <div className="admin-credit-entity"><strong>{value || "未识别模型"}</strong><AdminStatusBadge label={capabilityLabel(row.capability)} tone="info" /></div>,
        },
        { title: "区间消耗", dataIndex: "totalMicrocredits", width: 145, align: "right", render: (value) => <CreditValue value={value} /> },
        { title: "消耗占比", dataIndex: "totalMicrocredits", width: 150, render: (value) => <ConsumptionShare value={value} total={periodTotal} /> },
        { title: "结算任务", dataIndex: "orderCount", width: 100, align: "right", render: formatNumber },
        { title: "任务均耗", width: 120, align: "right", render: (_, row) => <span className="tabular-nums">{formatCredits(safeDivide(row.totalMicrocredits, row.orderCount), 6)}</span> },
        { title: "使用用户", dataIndex: "uniqueUsers", width: 100, align: "right", render: formatNumber },
        { title: "最近消耗", dataIndex: "lastConsumedAt", width: 160, render: formatDateTime },
        { title: "操作", key: "actions", fixed: "right", width: 100, render: (_, row) => <Button type="link" size="small" onClick={() => drillIntoModel(row.model, row.capability)}>查看明细</Button> },
    ];

    return (
        <AdminPageFrame
            title="积分消耗统计"
            description="从总量、效率、能力、用户与模型五个维度审视已结算积分消耗"
            scroll
            actions={<div className="admin-credit-page-actions">{updatedAt ? <span>更新于 {updatedAt.format("HH:mm:ss")}</span> : null}{filtered ? <Button icon={<RotateCcw className="size-4" />} onClick={resetFilters}>清除下钻</Button> : null}<Button icon={<RefreshCw className="size-4" />} loading={loading} onClick={() => void reload()}>刷新</Button></div>}
        >
            <div className="admin-credit-consumption py-4">
                <section className="admin-credit-filter-panel">
                    <div className="admin-credit-filter-heading">
                        <div><CalendarDays /><div><strong>统计范围</strong><span>最长支持 366 天，结束日期包含当天</span></div></div>
                        <div className="admin-credit-range-presets">
                            {rangePresets.map((preset) => <button key={preset.days} type="button" data-active={periodDays === preset.days} onClick={() => selectRange(preset.days)}>{preset.label}</button>)}
                        </div>
                    </div>
                    <div className="admin-credit-filter-controls">
                        <label><span>统计日期</span><RangePicker value={range} allowClear={false} onChange={(value) => { if (!value?.[0] || !value[1]) return; setRange([value[0], value[1]]); resetPages(); }} /></label>
                        <label><span>能力类型</span><Select allowClear placeholder="全部能力" value={capability || undefined} options={capabilityOptions} onChange={(value) => { setCapability(value || ""); resetPages(); }} /></label>
                        <label className="admin-credit-model-filter"><span>模型标识</span><Input.Search allowClear placeholder="输入完整模型标识" value={modelDraft} enterButton="应用" onChange={(event) => setModelDraft(event.target.value)} onSearch={applyModelFilter} /></label>
                    </div>
                    {filtered ? (
                        <div className="admin-credit-active-filters"><span>当前下钻</span>{userId ? <Tag closable onClose={() => setUserId("")}>用户：{selectedUserLabel(data, userId)}</Tag> : null}{model ? <Tag closable onClose={() => { setModel(""); setModelDraft(""); }}>模型：{model}</Tag> : null}{capability ? <Tag closable onClose={() => setCapability("")}>能力：{capabilityLabel(capability)}</Tag> : null}</div>
                    ) : null}
                </section>

                <section className="admin-credit-overview-grid">
                    <div className="admin-credit-primary-stat">
                        <div className="admin-credit-stat-icon"><Coins /></div>
                        <div className="admin-credit-primary-label">{filtered ? "当前筛选消耗" : "所选区间总消耗"}</div>
                        <div className="admin-credit-primary-value"><strong>{summary ? formatCredits(summary.periodMicrocredits, 6) : "--"}</strong><span>积分</span></div>
                        <Comparison current={summary?.periodMicrocredits || 0} previous={summary?.previousPeriodMicrocredits || 0} />
                        <div className="admin-credit-primary-footer"><span>{filters.from} 至 {filters.to}</span><span>历史累计 {summary ? formatCredits(summary.allTimeMicrocredits, 6) : "--"}</span></div>
                    </div>
                    <MetricCard icon={<Activity />} label="结算任务" value={formatNumber(summary?.settledOrders)} detail="不含预授权与退款" comparison={<Comparison current={summary?.settledOrders || 0} previous={summary?.previousSettledOrders || 0} compact />} />
                    <MetricCard icon={<UsersRound />} label="消耗用户" value={formatNumber(summary?.consumingUsers)} detail="区间去重用户" comparison={<Comparison current={summary?.consumingUsers || 0} previous={summary?.previousConsumingUsers || 0} compact />} />
                    <MetricCard icon={<Layers3 />} label="使用模型" value={formatNumber(summary?.usedModels)} detail="区间去重模型" comparison={<Comparison current={summary?.usedModels || 0} previous={summary?.previousUsedModels || 0} compact />} />
                    <MetricCard icon={<Target />} label="任务均耗" value={summary ? formatCredits(orderAverage, 6) : "--"} detail="每个已结算任务" />
                </section>

                <section className="admin-credit-insight-strip">
                    <Insight icon={<CalendarDays />} label="自然日均消耗" value={`${formatCredits(dailyAverage, 6)} 积分`} />
                    <Insight icon={<CircleDollarSign />} label="用户人均消耗" value={`${formatCredits(userAverage, 6)} 积分`} />
                    <Insight icon={<Clock3 />} label="活跃结算日" value={`${activeDays} / ${periodDays} 天`} />
                    <Insight icon={<Sparkles />} label="峰值日期" value={peakDay ? `${peakDay.day.slice(5)} · ${formatCredits(peakDay.totalMicrocredits, 6)}` : "暂无"} />
                </section>

                <div className="admin-credit-analysis-grid">
                    <section className="admin-credit-panel admin-credit-trend-panel">
                        <SectionHeading icon={<BarChart3 />} title="每日消耗趋势" description="面积表示积分消耗，柱形表示已结算任务量" />
                        {trend.length ? (
                            <div className="admin-credit-chart"><ResponsiveContainer width="100%" height="100%"><ComposedChart data={trend} margin={{ top: 10, right: 8, bottom: 0, left: 0 }}>
                                <defs><linearGradient id="creditTrendFill" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="var(--admin-chart-primary)" stopOpacity={0.28} /><stop offset="100%" stopColor="var(--admin-chart-primary)" stopOpacity={0.02} /></linearGradient></defs>
                                <CartesianGrid stroke="var(--admin-divider)" vertical={false} />
                                <XAxis dataKey="day" tickFormatter={(value) => String(value).slice(5)} tick={{ fontSize: 11 }} tickLine={false} axisLine={false} minTickGap={22} />
                                <YAxis yAxisId="credits" tick={{ fontSize: 11 }} tickLine={false} axisLine={false} width={46} />
                                <YAxis yAxisId="orders" orientation="right" tick={{ fontSize: 11 }} tickLine={false} axisLine={false} width={34} allowDecimals={false} />
                                <ChartTooltip labelFormatter={(value) => `日期 ${value}`} formatter={(value, name) => name === "积分消耗" ? [`${Number(value).toLocaleString("zh-CN", { maximumFractionDigits: 6 })} 积分`, name] : [Number(value).toLocaleString("zh-CN"), name]} />
                                <Legend iconType="circle" iconSize={7} wrapperStyle={{ fontSize: 11 }} />
                                <Area yAxisId="credits" type="monotone" dataKey="credits" name="积分消耗" stroke="var(--admin-chart-primary)" strokeWidth={2} fill="url(#creditTrendFill)" />
                                <Bar yAxisId="orders" dataKey="orderCount" name="结算任务" fill="var(--admin-chart-secondary)" fillOpacity={0.42} maxBarSize={18} radius={[3, 3, 0, 0]} />
                            </ComposedChart></ResponsiveContainer></div>
                        ) : <ChartEmpty />}
                    </section>

                    <section className="admin-credit-panel admin-credit-capability-panel">
                        <SectionHeading icon={<Layers3 />} title="能力消费构成" description="完整聚合全部能力类型，不受表格分页影响" />
                        <div className="admin-credit-capability-list">
                            {(data?.capabilities || []).map((item) => (
                                <button key={item.capability || "unknown"} type="button" data-active={capability === item.capability} onClick={() => { setCapability(item.capability); resetPages(); }}>
                                    <div className="admin-credit-capability-row"><span className="admin-credit-capability-name" data-capability={item.capability || "unknown"}>{capabilityLabel(item.capability)}</span><strong>{formatCredits(item.totalMicrocredits, 6)}</strong></div>
                                    <div className="admin-credit-capability-track"><span style={{ width: `${percentOf(item.totalMicrocredits, periodTotal)}%` }} /></div>
                                    <div className="admin-credit-capability-meta"><span>{formatPercentOf(item.totalMicrocredits, periodTotal)} 消耗</span><span>{formatNumber(item.orderCount)} 任务 · {formatNumber(item.uniqueUsers)} 用户 · {formatNumber(item.modelCount)} 模型</span></div>
                                </button>
                            ))}
                            {!data?.capabilities.length ? <div className="admin-credit-capability-empty">当前范围暂无能力消费构成</div> : null}
                        </div>
                    </section>
                </div>

                <section className="admin-credit-ranking-section">
                    <SectionHeading icon={<UsersRound />} title="用户消耗榜" description={`共 ${formatNumber(data?.users.total)} 位消费用户，按实际结算金额降序`} />
                    <AdminDataTable table={{ rowKey: "userId", size: "small", loading, columns: userColumns, dataSource: data?.users.items || [], scroll: { x: 1240 }, pagination: { current: userPage, pageSize, total: data?.users.total || 0, showSizeChanger: false, showTotal: (total) => `共 ${total} 位用户`, onChange: setUserPage } }} empty={<AdminTableEmpty title="当前范围暂无用户消耗" />} skeletonColumns={9} />
                </section>

                <section className="admin-credit-ranking-section">
                    <SectionHeading icon={<Sparkles />} title="模型消耗榜" description={`共 ${formatNumber(data?.models.total)} 个模型与能力组合，支持继续下钻`} />
                    <AdminDataTable table={{ rowKey: (row) => `${row.model}:${row.capability}`, size: "small", loading, columns: modelColumns, dataSource: data?.models.items || [], scroll: { x: 1240 }, pagination: { current: modelPage, pageSize, total: data?.models.total || 0, showSizeChanger: false, showTotal: (total) => `共 ${total} 个模型`, onChange: setModelPage } }} empty={<AdminTableEmpty title="当前范围暂无模型消耗" />} skeletonColumns={9} />
                </section>
            </div>
        </AdminPageFrame>
    );
}

function MetricCard({ icon, label, value, detail, comparison }: { icon: ReactNode; label: string; value: string; detail: string; comparison?: ReactNode }) {
    return <div className="admin-credit-metric-card"><div className="admin-credit-stat-icon">{icon}</div><span>{label}</span><strong>{value}</strong><small>{detail}</small>{comparison}</div>;
}

function Comparison({ current, previous, compact = false }: { current: number; previous: number; compact?: boolean }) {
    const change = comparisonChange(current, previous);
    return <span className="admin-credit-comparison" data-direction={change.direction} data-compact={compact}>{change.direction === "up" ? <ArrowUpRight /> : change.direction === "down" ? <ArrowDownRight /> : null}{change.label}</span>;
}

function Insight({ icon, label, value }: { icon: ReactNode; label: string; value: string }) {
    return <div className="admin-credit-insight"><span>{icon}</span><div><small>{label}</small><strong>{value}</strong></div></div>;
}

function SectionHeading({ icon, title, description }: { icon: ReactNode; title: string; description: string }) {
    return <div className="admin-credit-section-heading"><span>{icon}</span><div><h2>{title}</h2><p>{description}</p></div></div>;
}

function CreditValue({ value }: { value: number }) {
    return <span className="admin-credit-value tabular-nums">{formatCredits(value, 6)}</span>;
}

function ConsumptionShare({ value, total }: { value: number; total: number }) {
    return <div className="admin-credit-table-share"><span>{formatPercentOf(value, total)}</span><div><i style={{ width: `${percentOf(value, total)}%` }} /></div></div>;
}

function ChartEmpty() {
    return <div className="admin-credit-chart-empty"><BarChart3 /><strong>当前范围暂无已结算消耗</strong><span>调整日期或清除下钻后再查看</span></div>;
}

function comparisonChange(current: number, previous: number) {
    if (previous <= 0) return current > 0 ? { label: "上期无记录", direction: "neutral" } : { label: "与上期持平", direction: "neutral" };
    const value = ((current - previous) / previous) * 100;
    if (Math.abs(value) < 0.05) return { label: "与上期持平", direction: "neutral" };
    return { label: `较上期 ${value > 0 ? "+" : ""}${value.toLocaleString("zh-CN", { maximumFractionDigits: 1 })}%`, direction: value > 0 ? "up" : "down" };
}

function safeDivide(value: number, divisor: number) {
    return divisor > 0 ? Math.round(value / divisor) : 0;
}

function percentOf(value: number, total: number) {
    return total > 0 ? Math.min(100, Math.max(0, (value / total) * 100)) : 0;
}

function formatPercentOf(value: number, total: number) {
    return `${percentOf(value, total).toLocaleString("zh-CN", { maximumFractionDigits: 1 })}%`;
}

function formatNumber(value?: number) {
    return value === undefined || value === null ? "--" : value.toLocaleString("zh-CN");
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
