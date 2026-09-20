import { Alert, App, Button, DatePicker, Select, Tabs, Tag } from "antd";
import { Tooltip } from "@/pages/admin/ui/controls";
import { useCountUp } from "@/hooks/use-count-up";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import type { ColumnsType } from "antd/es/table";
import dayjs, { type Dayjs } from "dayjs";
import { Activity, AlertTriangle, BarChart3, CalendarDays, CircleDollarSign, Clock3, Coins, Film, Gauge, Hash, Image, Layers3, MessageSquareText, Music2, RefreshCw, Sparkles, Target, UsersRound, Workflow } from "lucide-react";
import { Area, Bar, CartesianGrid, ComposedChart, Legend, Line, ResponsiveContainer, Tooltip as ChartTooltip, XAxis, YAxis } from "recharts";
import { useSearchParams } from "react-router";

import { ListToolbar, PaginationBar, AdminDataTable, AdminExportButton, AdminFilterChip, AdminStatusBadge, AdminTableEmpty, type AdminStatusTone } from "./admin-ui";
import { analyticsFinanceColumns, formatCredits as formatFinanceCredits, formatFinanceCost, formatFinanceMargin } from "./analytics-finance";
import {
    exportAdminAnalytics,
    getAdminAnalytics,
    listAdminUsers,
    type AdminReferenceData,
    type AdminAnalytics,
    type AnalyticsFilters,
} from "@/services/api/auth";

type Props = {
    users: AdminReferenceData["users"];
    channels: AdminReferenceData["channels"];
};

type TrendMetric = "credits" | "volume" | "output" | "quality" | "activity";
type AnalysisTab = "models" | "channels" | "users" | "failures";
type RangePreset = "7d" | "30d" | "90d";

const capabilityOptions = [
    { label: "文本", value: "text" },
    { label: "图片", value: "image" },
    { label: "视频", value: "video" },
    { label: "音频", value: "audio" },
];

export default function AnalyticsPanel({ users, channels }: Props) {
    const { message } = App.useApp();
    const [searchParams, setSearchParams] = useSearchParams();
    const [rangePreset, setRangePreset] = useState<RangePreset | undefined>(() => initialRangePreset(searchParams));
    const [range, setRange] = useState<[Dayjs, Dayjs]>(() => initialAnalyticsRange(searchParams, rangePreset));
    const [userId, setUserId] = useState(searchParams.get("userId") || undefined);
    const [model, setModel] = useState(searchParams.get("model") || undefined);
    const [channelId, setChannelId] = useState(searchParams.get("channelId") || undefined);
    const [capability, setCapability] = useState(searchParams.get("capability") || undefined);
    const [data, setData] = useState<AdminAnalytics | null>(null);
    const [previousData, setPreviousData] = useState<AdminAnalytics | null>(null);
    const [lastUpdatedAt, setLastUpdatedAt] = useState<Dayjs | null>(null);
    const [loading, setLoading] = useState(false);
    const [userOptions, setUserOptions] = useState(users);
    const [searchingUsers, setSearchingUsers] = useState(false);
    const [modelPage, setModelPage] = useState(1);
    const [channelPage, setChannelPage] = useState(1);
    const [userPage, setUserPage] = useState(1);
    const [failurePage, setFailurePage] = useState(1);
    const [trendMetric, setTrendMetric] = useState<TrendMetric>("credits");
    const [analysisTab, setAnalysisTab] = useState<AnalysisTab>("users");
    const analyticsPageSize = 20;

    const filters = useMemo<AnalyticsFilters>(
        () => ({
            from: range[0].format("YYYY-MM-DD"),
            to: range[1].format("YYYY-MM-DD"),
            userId,
            model,
            channelId,
            capability,
        }),
        [capability, channelId, model, range, userId],
    );
    const previousFilters = useMemo<AnalyticsFilters>(() => {
        const days = Math.max(1, range[1].startOf("day").diff(range[0].startOf("day"), "day") + 1);
        const previousEnd = range[0].subtract(1, "day");
        return {
            ...filters,
            from: previousEnd.subtract(days - 1, "day").format("YYYY-MM-DD"),
            to: previousEnd.format("YYYY-MM-DD"),
        };
    }, [filters, range]);

    const reload = useCallback(async () => {
        setLoading(true);
        try {
            const [analytics, previousAnalytics] = await Promise.all([getAdminAnalytics(filters), getAdminAnalytics(previousFilters)]);
            setData(analytics);
            setPreviousData(previousAnalytics);
            setLastUpdatedAt(dayjs());
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取统计数据失败");
        } finally {
            setLoading(false);
        }
    }, [filters, message, previousFilters]);

    useEffect(() => {
        const next = new URLSearchParams(searchParams);
        for (const [key, value] of Object.entries(filters)) {
            if (value) next.set(key, value);
            else next.delete(key);
        }
        if (rangePreset) next.set("rangePreset", rangePreset);
        else next.delete("rangePreset");
        setSearchParams(next, { replace: true });
        void reload();
    }, [filters, rangePreset]);

    useEffect(() => {
        setModelPage(1);
        setChannelPage(1);
        setUserPage(1);
        setFailurePage(1);
    }, [filters]);

    useEffect(() => {
        setUserOptions(users);
    }, [users]);

    const searchUsers = async (keyword: string) => {
        setSearchingUsers(true);
        try {
            const result = await listAdminUsers({ keyword: keyword.trim() || undefined, page: 1, pageSize: 50 });
            setUserOptions(result.users);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "搜索用户失败");
        } finally {
            setSearchingUsers(false);
        }
    };

    const modelOptions = useMemo(() => {
        const names = new Set<string>();
        channels.forEach((channel) => channel.models?.forEach((name) => names.add(name)));
        data?.models.forEach((item) => item.model !== "未识别" && names.add(item.model));
        return [...names].sort().map((name) => ({ label: name, value: name }));
    }, [channels, data?.models]);

    const modelColumns: ColumnsType<AdminAnalytics["models"][number]> = [
        {
            title: "模型",
            dataIndex: "model",
            fixed: "left",
            width: 210,
            render: (value, row) => (
                <div>
                    <div className="font-medium">{value}</div>
                    <div className="mt-1">
                        <Tag className="admin-analytics-capability-tag">{capabilityLabel(row.capability)}</Tag>
                    </div>
                </div>
            ),
        },
        { title: "任务 / 请求", width: 120, render: (_, row) => `${row.tasks} / ${row.requests}` },
        { title: "任务状态", width: 155, render: (_, row) => `${row.succeededTasks} 成功 / ${row.failedTasks} 失败 / ${row.queuedTasks + row.runningTasks} 处理中` },
        { title: "用户", dataIndex: "uniqueUsers", width: 80 },
        { title: "任务成功率", dataIndex: "taskSuccessRate", width: 110, render: percent },
        { title: "请求成功率", dataIndex: "requestSuccessRate", width: 110, render: percent },
        { title: "P50 / P95", width: 145, render: (_, row) => `${formatDuration(row.p50DurationMs)} / ${formatDuration(row.p95DurationMs)}` },
        { title: "Token（入 / 出 / 缓存）", width: 190, render: (_, row) => (row.usageAvailable ? `${formatNumber(row.inputTokens)} / ${formatNumber(row.outputTokens)} / ${formatNumber(row.cachedTokens)}` : "--") },
        { title: "图 / 视 / 音", width: 125, render: (_, row) => `${row.generatedImages} / ${row.generatedVideos} / ${row.generatedAudio}` },
        { title: "视频秒数", width: 100, dataIndex: "videoSeconds" },
        { title: "视频平均积分/秒", width: 145, render: (_, row) => (row.capability === "video" && row.videoSeconds > 0 ? `${formatCreditsValue(row.creditsConsumedMicrocredits / row.videoSeconds)} 积分/秒` : "--") },
        { title: "消耗积分", width: 115, render: (_, row) => formatCredits(row.creditsConsumedMicrocredits) },
        ...analyticsFinanceColumns,
    ];

    const userColumns: ColumnsType<AdminAnalytics["users"][number]> = [
        {
            title: "用户",
            dataIndex: "name",
            width: 180,
            render: (name, row) => (
                <div>
                    <div className="font-medium">{name}</div>
                    <div className="text-xs text-foreground/45">{row.userId}</div>
                </div>
            ),
        },
        { title: "活跃 / 登录", width: 110, render: (_, row) => `${row.activeDays} 天 / ${row.loginCount} 次` },
        { title: "用量（任务 / 请求 / 媒体）", width: 175, render: (_, row) => `${row.tasks} / ${row.requests} / ${row.mediaCount}` },
        { title: "文 / 图 / 视 / 音", width: 145, render: (_, row) => `${row.textTasks} / ${row.imageTasks} / ${row.videoTasks} / ${row.audioTasks}` },
        { title: "任务状态", width: 180, render: (_, row) => `${row.succeededTasks} 成功 / ${row.failedTasks} 失败 / ${row.cancelledTasks} 取消 / ${row.queuedTasks + row.runningTasks} 处理中` },
        { title: "任务质量", width: 175, render: (_, row) => `${percent(row.taskSuccessRate)} · 均 ${formatDuration(row.averageTaskDurationMs)} · P95 ${formatDuration(row.p95TaskDurationMs)}` },
        { title: "请求质量", width: 175, render: (_, row) => `${row.succeededRequests} 成功 / ${row.failedRequests} 失败 · ${percent(row.requestSuccessRate)}` },
        { title: "请求 P95", width: 105, render: (_, row) => formatDuration(row.p95RequestDurationMs) },
        { title: "图 / 视 / 音", width: 125, render: (_, row) => `${row.generatedImages} / ${row.generatedVideos} / ${row.generatedAudio}` },
        { title: "视频秒数", width: 100, dataIndex: "videoSeconds" },
        { title: "视频积分", width: 115, render: (_, row) => formatCredits(videoCreditsForRows(row.models)) },
        { title: "视频平均积分/秒", width: 145, render: (_, row) => formatAverageVideoCredits(row.models, row.videoSeconds) },
        { title: "Token（入 / 出 / 缓存）", width: 190, render: (_, row) => (row.usageAvailable ? `${formatNumber(row.inputTokens)} / ${formatNumber(row.outputTokens)} / ${formatNumber(row.cachedTokens)}` : "--") },
        { title: "总积分用量", width: 125, render: (_, row) => formatCredits(row.creditsConsumedMicrocredits) },
        { title: "上游估算费用", width: 130, render: (_, row) => formatCost(row.estimatedCostMicros, row.currency, row.costAvailable) },
        { title: "Agent / 画布天数", width: 145, render: (_, row) => `${row.agentMessages} / ${row.canvasDays}` },
        { title: "素材 / 资源", width: 110, render: (_, row) => `${row.assets} / ${row.resources}` },
        { title: "最后活跃", dataIndex: "lastActiveAt", width: 150, render: formatAnalyticsTime },
        { title: "常用模型", dataIndex: "commonModel", width: 180, ellipsis: true, render: (value) => value || "--" },
    ];

    const channelColumns: ColumnsType<AdminAnalytics["channels"][number]> = [
        {
            title: "渠道",
            dataIndex: "name",
            fixed: "left",
            width: 200,
            render: (value, row) => (
                <div>
                    <div className="font-medium">{value}</div>
                    <div className="text-xs text-foreground/45">{row.channelId || "无系统渠道 ID"}</div>
                </div>
            ),
        },
        { title: "任务 / 请求", width: 120, render: (_, row) => `${row.tasks} / ${row.requests}` },
        { title: "用户 / 模型", width: 120, render: (_, row) => `${row.uniqueUsers} / ${row.uniqueModels}` },
        { title: "请求成 / 败", width: 125, render: (_, row) => `${row.succeededRequests} / ${row.failedRequests}` },
        { title: "请求成功率", width: 110, render: (_, row) => percent(row.requestSuccessRate) },
        { title: "P50 / P95", width: 145, render: (_, row) => `${formatDuration(row.p50DurationMs)} / ${formatDuration(row.p95DurationMs)}` },
        { title: "图 / 视 / 音", width: 125, render: (_, row) => `${row.generatedImages} / ${row.generatedVideos} / ${row.generatedAudio}` },
        { title: "视频秒数", dataIndex: "videoSeconds", width: 100 },
        { title: "Token（入 / 出 / 缓存）", width: 190, render: (_, row) => (row.usageAvailable ? `${formatNumber(row.inputTokens)} / ${formatNumber(row.outputTokens)} / ${formatNumber(row.cachedTokens)}` : "未返回") },
        { title: "消耗积分", width: 115, render: (_, row) => formatCredits(row.creditsConsumedMicrocredits) },
        { title: "估算费用", width: 120, render: (_, row) => formatCost(row.estimatedCostMicros, row.currency, row.costAvailable) },
    ];

    const failureColumns: ColumnsType<AdminAnalytics["failures"][number]> = [
        { title: "错误类型", dataIndex: "type", width: 120, render: (value) => <AdminStatusBadge label={value} tone={value === "超时" ? "warning" : "error"} /> },
        { title: "模型", dataIndex: "model", width: 220 },
        { title: "次数", dataIndex: "count", width: 90 },
        { title: "最近错误", dataIndex: "lastError", ellipsis: true, render: (value) => <Tooltip title={value}>{value || "--"}</Tooltip> },
        { title: "最近发生", dataIndex: "lastSeenAt", width: 170, render: (value) => dayjs(value).format("YYYY-MM-DD HH:mm") },
    ];

    const trend = data?.trend || [];
    const currentTrend = trend[trend.length - 1];
    const previousTrend = trend[trend.length - 2];
    const modelRows = data?.models || [];
    const channelRows = data?.channels || [];
    const userRows = data?.users || [];
    const failureRows = data?.failures || [];
    const failureTotal = failureRows.reduce((sum, item) => sum + item.count, 0);
    const topFailure = failureRows.reduce<AdminAnalytics["failures"][number] | undefined>((current, item) => (!current || item.count > current.count ? item : current), undefined);
    const finance = data?.kpi.finance;
    const financeUnavailable = data !== null && (!finance || modelRows.some((row) => !row.finance));
    const rangeDays = Math.max(1, range[1].startOf("day").diff(range[0].startOf("day"), "day") + 1);
    const creditUsers = userRows.filter((item) => item.creditsConsumedMicrocredits > 0);
    const creditedModelCount = new Set(modelRows.filter((item) => item.creditsConsumedMicrocredits > 0).map((item) => item.model)).size;
    const dailyCredits = new Map<string, number>();
    userRows.forEach((item) => item.daily.forEach((day) => dailyCredits.set(day.day, (dailyCredits.get(day.day) || 0) + day.creditsConsumedMicrocredits)));
    const consumptionTrend = trend.map((item) => ({ ...item, credits: fromMicros(dailyCredits.get(item.day) || 0) }));
    const activeSettlementDays = consumptionTrend.filter((item) => item.credits > 0).length;
    const peakSettlement = consumptionTrend.reduce<(typeof consumptionTrend)[number] | undefined>((peak, item) => (!peak || item.credits > peak.credits ? item : peak), undefined);
    const capabilityBreakdown = capabilityOptions.map((option) => {
        const rows = modelRows.filter((item) => item.capability === option.value);
        const credits = rows.reduce((sum, item) => sum + item.creditsConsumedMicrocredits, 0);
        const tasks = rows.reduce((sum, item) => sum + item.tasks, 0);
        const capabilityUsers = userRows.filter((item) => item.models.some((modelItem) => modelItem.capability === option.value && (modelItem.tasks > 0 || modelItem.creditsConsumedMicrocredits > 0))).length;
        return { capability: option.value, label: option.label, credits, tasks, users: capabilityUsers, models: rows.length };
    });
    const videoCredits = videoCreditsForRows(modelRows);
    const videoAverageCredits = averageVideoCreditsPerSecond(modelRows, data?.kpi.videoSeconds || 0);
    const previousVideoAverageCredits = averageVideoCreditsPerSecond(previousData?.models || [], previousData?.kpi.videoSeconds || 0);
    const rankedUsers = [...userRows].sort((left, right) => right.creditsConsumedMicrocredits - left.creditsConsumedMicrocredits || right.videoSeconds - left.videoSeconds || left.name.localeCompare(right.name, "zh-CN"));
    const maxCapabilityCredits = Math.max(...capabilityBreakdown.map((item) => item.credits), 1);
    const trendHasData =
        trendMetric === "credits"
            ? consumptionTrend.some((item) => item.credits > 0 || item.tasks > 0)
            : trendMetric === "volume"
            ? trend.some((item) => item.tasks > 0 || item.requests > 0)
            : trendMetric === "output"
              ? trend.some((item) => item.mediaCount > 0 || item.videoSeconds > 0)
              : trendMetric === "quality"
                ? trend.some((item) => item.succeededTasks > 0 || item.failedTasks > 0 || item.requests > 0)
                : trend.some((item) => item.activeUsers > 0);
    const trendTitle = trendMetric === "credits" ? "每日消耗趋势" : trendMetric === "volume" ? "任务与请求趋势" : trendMetric === "output" ? "媒体产出趋势" : trendMetric === "quality" ? "生成质量趋势" : "用户活跃趋势";
    const pageRows = <T,>(rows: T[], page: number) => rows.slice((page - 1) * analyticsPageSize, page * analyticsPageSize);

    const applyRangePreset = (preset: RangePreset) => {
        const end = dayjs();
        const start = preset === "7d" ? end.subtract(6, "day") : preset === "30d" ? end.subtract(29, "day") : end.subtract(89, "day");
        setRangePreset(preset);
        setRange([start, end]);
    };

    const openAnalysis = (tab: AnalysisTab) => {
        setAnalysisTab(tab);
        window.requestAnimationFrame(() => document.getElementById("admin-analytics-analysis")?.scrollIntoView({ behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth", block: "start" }));
    };

    return (
        <div className="admin-analytics-panel space-y-5">
            <section className="admin-analytics-scope" aria-labelledby="admin-analytics-scope-title">
                <div className="admin-analytics-scope-heading">
                    <div className="admin-analytics-scope-title">
                        <span aria-hidden="true"><CalendarDays className="size-4" /></span>
                        <div>
                            <h2 id="admin-analytics-scope-title">统计范围</h2>
                            <p>最长支持 366 天，结束日期包含当天</p>
                        </div>
                    </div>
                    <div className="admin-analytics-range-presets" role="group" aria-label="快捷时间范围">
                        {([
                            ["7d", "近 7 天"],
                            ["30d", "近 30 天"],
                            ["90d", "近 90 天"],
                        ] as const).map(([value, label]) => (
                            <button key={value} type="button" className={rangePreset === value ? "is-active" : undefined} aria-pressed={rangePreset === value} onClick={() => applyRangePreset(value)}>{label}</button>
                        ))}
                    </div>
                </div>
                <div className="admin-analytics-scope-fields">
                    <div className="admin-analytics-field admin-analytics-date-field">
                        <span>统计日期</span>
                        <DatePicker.RangePicker
                            allowClear={false}
                            value={range}
                            onChange={(value) => {
                                if (value?.[0] && value?.[1]) {
                                    setRangePreset(undefined);
                                    setRange([value[0], value[1]]);
                                }
                            }}
                        />
                    </div>
                    <FilterSelect label="能力类型" value={capability} onChange={setCapability} options={capabilityOptions} />
                    <FilterSelect label="模型标识" value={model} onChange={setModel} options={modelOptions} />
                    <FilterSelect label="渠道" value={channelId} onChange={setChannelId} options={channels.map((channel) => ({ label: channel.name, value: channel.id }))} />
                    <FilterSelect
                        label="用户"
                        value={userId}
                        onChange={setUserId}
                        options={userOptions.map((user) => ({ label: user.displayName || user.username, value: user.id }))}
                        filterOption={false}
                        loading={searchingUsers}
                        onSearch={(value) => void searchUsers(value)}
                    />
                </div>
                <div className="admin-analytics-scope-actions">
                    <span className="admin-analytics-updated-at">{lastUpdatedAt ? `更新于 ${lastUpdatedAt.format("HH:mm:ss")}` : "尚未更新"}</span>
                    <Button onClick={() => { setUserId(undefined); setModel(undefined); setChannelId(undefined); setCapability(undefined); }}>重置筛选</Button>
                    <Button icon={<RefreshCw className="size-4" />} loading={loading} onClick={() => void reload()}>刷新</Button>
                    <AdminExportButton exportFile={() => exportAdminAnalytics(filters)} fileName={() => `usage-${filters.from}-${filters.to}.csv`} label="导出 CSV" />
                </div>
            </section>

            <section className="admin-analytics-consumption-grid" aria-label="积分消耗总览">
                <AnalyticsConsumptionCard
                    featured
                    icon={<Coins className="size-4" />}
                    label="所选区间总消耗"
                    value={data ? formatCreditsValue(data.kpi.creditsConsumedMicrocredits) : "--"}
                    unit="积分"
                    comparison={formatPeriodDelta(data?.kpi.creditsConsumedMicrocredits, previousData?.kpi.creditsConsumedMicrocredits)}
                    footer={`${range[0].format("YYYY-MM-DD")} 至 ${range[1].format("YYYY-MM-DD")}`}
                    footerValue={previousData ? `上周期 ${formatCreditsValue(previousData.kpi.creditsConsumedMicrocredits)}` : undefined}
                />
                <AnalyticsConsumptionCard icon={<Activity className="size-4" />} label="生成任务" value={data ? formatNumber(data.kpi.generationTasks) : "--"} comparison={formatPeriodDelta(data?.kpi.generationTasks, previousData?.kpi.generationTasks)} footer={`成功 ${data?.kpi.succeededTasks || 0} · 失败 ${data?.kpi.failedTasks || 0}`} />
                <AnalyticsConsumptionCard icon={<UsersRound className="size-4" />} label="消费用户" value={data ? formatNumber(creditUsers.length) : "--"} comparison={formatPeriodDelta(creditUsers.length, previousData?.users.filter((item) => item.creditsConsumedMicrocredits > 0).length)} footer={`活跃用户 ${data?.kpi.activeUsers || 0}`} />
                <AnalyticsConsumptionCard icon={<Film className="size-4" />} label="视频生成秒数" value={data ? formatNumber(data.kpi.videoSeconds) : "--"} unit="秒" comparison={formatPeriodDelta(data?.kpi.videoSeconds, previousData?.kpi.videoSeconds)} footer={`${formatNumber(data?.kpi.generatedVideos || 0)} 个成功视频`} />
                <AnalyticsConsumptionCard icon={<CircleDollarSign className="size-4" />} label="视频平均积分/秒" value={videoAverageCredits === undefined ? "--" : formatCreditsValue(videoAverageCredits)} unit="积分/秒" comparison={formatPeriodDelta(videoAverageCredits, previousVideoAverageCredits)} footer={`视频已结算 ${formatCreditsValue(videoCredits)} 积分`} />
                <AnalyticsConsumptionCard icon={<Layers3 className="size-4" />} label="使用模型" value={data ? formatNumber(creditedModelCount) : "--"} footer={`区间共 ${modelRows.length} 个模型能力组合`} />
                <AnalyticsConsumptionCard icon={<Target className="size-4" />} label="任务均耗" value={data?.kpi.generationTasks ? formatCreditsValue(data.kpi.creditsConsumedMicrocredits / data.kpi.generationTasks) : "0"} footer="每个生成任务的已结算积分" />
            </section>

            <section className="admin-analytics-secondary-metrics" aria-label="积分效率指标">
                <AnalyticsSecondaryMetric icon={<CalendarDays className="size-4" />} label="日均已结算" value={`${formatCreditsValue((data?.kpi.creditsConsumedMicrocredits || 0) / rangeDays)} 积分`} />
                <AnalyticsSecondaryMetric icon={<UsersRound className="size-4" />} label="消费用户均值" value={`${formatCreditsValue((data?.kpi.creditsConsumedMicrocredits || 0) / Math.max(creditUsers.length, 1))} 积分`} />
                <AnalyticsSecondaryMetric icon={<Clock3 className="size-4" />} label="活跃结算日" value={`${activeSettlementDays} / ${rangeDays} 天`} />
                <AnalyticsSecondaryMetric icon={<Sparkles className="size-4" />} label="峰值日期" value={peakSettlement?.credits ? `${peakSettlement.day.slice(5)} · ${formatCreditsValue(peakSettlement.credits * 1_000_000)}` : "暂无结算"} />
            </section>

            <section className="admin-analytics-user-usage" aria-labelledby="admin-analytics-user-usage-title">
                <div className="admin-analytics-user-usage-heading">
                    <div>
                        <h2 id="admin-analytics-user-usage-title">每个人的用量</h2>
                        <p>逐人汇总当前筛选范围内的任务、请求、媒体、视频秒数与已结算积分。</p>
                    </div>
                    <Button icon={<UsersRound className="size-4" />} onClick={() => openAnalysis("users")}>查看完整个人明细</Button>
                </div>
                <div className="admin-analytics-user-usage-list">
                    {rankedUsers.length ? rankedUsers.map((row) => {
                        const personalVideoCredits = videoCreditsForRows(row.models);
                        return (
                            <article key={row.userId} className="admin-analytics-user-usage-row">
                                <div className="admin-analytics-user-identity"><strong>{row.name}</strong><span>{row.userId}</span></div>
                                <AnalyticsUserUsageMetric label="任务 / 请求" value={`${formatNumber(row.tasks)} / ${formatNumber(row.requests)}`} />
                                <AnalyticsUserUsageMetric label="媒体产出" value={formatNumber(row.mediaCount)} />
                                <AnalyticsUserUsageMetric label="视频秒数" value={`${formatNumber(row.videoSeconds)} 秒`} />
                                <AnalyticsUserUsageMetric label="视频积分" value={formatCredits(personalVideoCredits)} />
                                <AnalyticsUserUsageMetric label="平均积分/秒" value={formatAverageVideoCredits(row.models, row.videoSeconds)} />
                                <AnalyticsUserUsageMetric label="总积分用量" value={formatCredits(row.creditsConsumedMicrocredits)} emphasized />
                            </article>
                        );
                    }) : <div className="admin-analytics-user-usage-empty">当前筛选范围内没有个人用量记录</div>}
                </div>
            </section>

            <div className="admin-analytics-overview-grid">
                <section className="admin-analytics-trend-section" aria-labelledby="admin-analytics-trend-title">
                    <div className="admin-analytics-section-heading">
                        <div>
                            <h2 id="admin-analytics-trend-title">{trendTitle}</h2>
                            <p>
                                {trendMetric === "credits"
                                    ? "柱形表示当日已结算积分，折线表示生成任务量。"
                                    : trendMetric === "volume"
                                    ? "生成任务与真实上游请求分开统计。"
                                    : trendMetric === "output"
                                      ? "媒体数量与视频秒数按成功任务统计。"
                                      : trendMetric === "quality"
                                        ? "任务与上游请求使用各自独立的成功率。"
                                        : "活跃用户按自然日去重统计。"}
                            </p>
                        </div>
                        <div className="admin-analytics-trend-switch" role="group" aria-label="趋势指标">
                            {(
                                [
                                    ["credits", "积分"],
                                    ["volume", "用量"],
                                    ["output", "产出"],
                                    ["quality", "质量"],
                                    ["activity", "活跃"],
                                ] as const
                            ).map(([value, label]) => (
                                <button key={value} type="button" className={trendMetric === value ? "is-active" : undefined} aria-pressed={trendMetric === value} onClick={() => setTrendMetric(value)}>
                                    {label}
                                </button>
                            ))}
                        </div>
                    </div>
                    {trendHasData ? (
                        <div className="admin-analytics-chart" role="img" aria-label={`${trendTitle}图`}>
                            <ResponsiveContainer width="100%" height="100%">
                                <ComposedChart data={consumptionTrend} margin={{ top: 8, right: 12, bottom: 0, left: -8 }}>
                                    <CartesianGrid stroke="currentColor" className="text-foreground/10" vertical={false} />
                                    <XAxis dataKey="day" tickFormatter={(value) => value.slice(5)} tick={{ fontSize: 11 }} />
                                    {trendMetric === "quality" ? <YAxis yAxisId="primary" domain={[0, 100]} tickFormatter={(value) => `${value}%`} tick={{ fontSize: 11 }} /> : <YAxis yAxisId="primary" allowDecimals={trendMetric === "credits"} tick={{ fontSize: 11 }} />}
                                    {trendMetric === "output" || trendMetric === "credits" ? <YAxis yAxisId="secondary" orientation="right" allowDecimals={false} tick={{ fontSize: 11 }} /> : null}
                                    <ChartTooltip labelFormatter={(value) => `日期 ${value}`} formatter={(value, name) => [name === "已结算积分" ? `${formatCreditsValue(Number(value) * 1_000_000)} 积分` : value, name]} />
                                    {trendMetric === "credits" || trendMetric === "volume" || trendMetric === "output" || trendMetric === "quality" ? <Legend wrapperStyle={{ fontSize: 12 }} /> : null}
                                    {trendMetric === "credits" ? <Bar yAxisId="primary" dataKey="credits" name="已结算积分" fill="var(--admin-chart-secondary)" radius={[3, 3, 0, 0]} maxBarSize={30} /> : null}
                                    {trendMetric === "credits" ? <Line yAxisId="secondary" type="monotone" dataKey="tasks" name="生成任务" stroke="var(--admin-text-primary)" dot={false} strokeWidth={2} /> : null}
                                    {trendMetric === "volume" ? (
                                        <>
                                            <Area yAxisId="primary" type="monotone" dataKey="tasks" name="生成任务" stroke="var(--admin-chart-primary)" fill="var(--admin-chart-primary)" fillOpacity={0.1} />
                                            <Area yAxisId="primary" type="monotone" dataKey="requests" name="上游请求" stroke="var(--admin-chart-secondary)" fill="var(--admin-chart-secondary)" fillOpacity={0.08} />
                                        </>
                                    ) : null}
                                    {trendMetric === "output" ? <Area yAxisId="primary" type="monotone" dataKey="mediaCount" name="媒体数量" stroke="var(--admin-chart-primary)" fill="var(--admin-chart-primary)" fillOpacity={0.1} /> : null}
                                    {trendMetric === "output" ? <Line yAxisId="secondary" type="monotone" dataKey="videoSeconds" name="视频秒数" stroke="var(--admin-chart-secondary)" dot={false} strokeWidth={2} /> : null}
                                    {trendMetric === "quality" ? <Line yAxisId="primary" type="monotone" dataKey="taskSuccessRate" name="任务成功率" stroke="var(--admin-chart-primary)" dot={false} strokeWidth={2} /> : null}
                                    {trendMetric === "quality" ? <Line yAxisId="primary" type="monotone" dataKey="requestSuccessRate" name="请求成功率" stroke="var(--admin-chart-warning)" dot={false} strokeWidth={2} /> : null}
                                    {trendMetric === "activity" ? <Area yAxisId="primary" type="monotone" dataKey="activeUsers" name="活跃用户" stroke="var(--admin-chart-primary)" fill="var(--admin-chart-primary)" fillOpacity={0.1} /> : null}
                                </ComposedChart>
                            </ResponsiveContainer>
                        </div>
                    ) : (
                        <div className="admin-analytics-empty-chart">
                            <span>
                                <BarChart3 className="size-5" />
                            </span>
                            <div className="font-medium">当前范围暂无{trendMetric === "credits" ? "积分结算" : trendMetric === "quality" ? "质量" : trendMetric === "output" ? "产出" : trendMetric === "activity" ? "活跃" : "使用"}数据</div>
                            <p>可以调整时间范围或筛选条件后重新查看。</p>
                        </div>
                    )}
                </section>

                <aside className="admin-analytics-attention admin-analytics-capability-mix" aria-labelledby="admin-analytics-attention-title">
                    <div className="admin-analytics-attention-heading">
                        <div>
                            <h2 id="admin-analytics-attention-title">能力消费构成</h2>
                            <p>按模型能力汇总已结算积分，不受表格分页影响。</p>
                        </div>
                        <Layers3 className="size-4" />
                    </div>
                    <div className="admin-analytics-capability-list">
                        {capabilityBreakdown.map((item) => (
                            <button key={item.capability} type="button" className={`admin-analytics-capability-item${capability === item.capability ? " is-active" : ""}`} aria-pressed={capability === item.capability} onClick={() => setCapability((current) => (current === item.capability ? undefined : item.capability))}>
                                <span className="admin-analytics-capability-row">
                                    <span className="admin-analytics-capability-label">{capabilityIcon(item.capability)} {item.label}</span>
                                    <strong>{formatCreditsValue(item.credits)}</strong>
                                </span>
                                <span className="admin-analytics-capability-track"><span style={{ width: `${Math.max(item.credits > 0 ? 2 : 0, (item.credits / maxCapabilityCredits) * 100)}%` }} /></span>
                                <span className="admin-analytics-capability-meta">{formatCapabilityShare(item.credits, data?.kpi.creditsConsumedMicrocredits)} · {formatNumber(item.tasks)} 任务 · {item.users} 用户 · {item.models} 模型</span>
                            </button>
                        ))}
                    </div>
                </aside>
            </div>

            <section className="admin-analytics-health-grid admin-analytics-health-grid-detailed" aria-label="运营健康指标">
                <AnalyticsHealthCard icon={<UsersRound className="size-4" />} label="活跃用户" value={data ? <AnimatedHealthValue target={data.kpi.activeUsers} format={formatNumber} /> : "--"} trend={formatCountDelta(currentTrend?.activeUsers, previousTrend?.activeUsers)} detail={data ? `DAU ${data.kpi.dau} · WAU ${data.kpi.wau} · MAU ${data.kpi.mau}` : undefined} />
                <AnalyticsHealthCard icon={<Film className="size-4" />} label="视频生成秒数" value={data ? `${formatNumber(data.kpi.videoSeconds)} 秒` : "--"} detail={data ? `${data.kpi.generatedVideos} 个视频 · ${data.kpi.generatedImages} 张图片 · ${data.kpi.generatedAudio} 段音频` : undefined} />
                <AnalyticsHealthCard icon={<Activity className="size-4" />} label="任务成功率" value={data ? percent(data.kpi.taskSuccessRate) : "--"} trend={formatRateDelta(currentTrend?.taskSuccessRate, previousTrend?.taskSuccessRate)} detail={data ? `${data.kpi.succeededTasks} 成功 · ${data.kpi.failedTasks} 失败 · ${data.kpi.cancelledTasks} 取消` : undefined} tone={data && data.kpi.taskSuccessRate < 90 ? "warning" : "success"} />
                <AnalyticsHealthCard icon={<Gauge className="size-4" />} label="上游请求" value={data ? formatNumber(data.kpi.upstreamRequests) : "--"} detail={data ? `${percent(data.kpi.successRate)} 成功率 · P95 ${formatDuration(data.kpi.p95DurationMs)}` : undefined} tone={data && data.kpi.successRate < 90 ? "warning" : "success"} />
                <AnalyticsHealthCard icon={<Hash className="size-4" />} label="Token 用量" value={data?.kpi.usageAvailable ? formatNumber(data.kpi.inputTokens + data.kpi.outputTokens) : "暂无"} detail={data?.kpi.usageAvailable ? `输入 ${formatNumber(data.kpi.inputTokens)} · 输出 ${formatNumber(data.kpi.outputTokens)} · 缓存 ${formatNumber(data.kpi.cachedTokens)}` : "上游未返回 Token 用量"} />
                <AnalyticsHealthCard icon={<AlertTriangle className="size-4" />} label="异常与队列" value={data ? `${failureTotal} / ${data.kpi.currentQueuedTasks}` : "--"} detail={topFailure ? `${topFailure.type} · ${topFailure.model}` : "异常请求 / 当前排队任务"} tone={failureTotal > 0 || Boolean(data?.kpi.currentQueuedTasks) ? "warning" : "success"} />
                <AnalyticsHealthCard
                    icon={<CircleDollarSign className="size-4" />}
                    label="结算财务（积分）"
                    value={finance ? <dl className="admin-analytics-finance-values">
                        <div><dt>收入</dt><dd>{formatFinanceCredits(finance.revenueMicrocredits)}</dd></div>
                        <div><dt>成本</dt><dd>{formatFinanceCost(finance)}</dd></div>
                        <div><dt>利润</dt><dd>{formatFinanceCredits(finance.profitMicrocredits)}</dd></div>
                    </dl> : "--"}
                    detail={finance ? `成本覆盖 ${finance.costedOrders}/${finance.settledOrders} 笔 · 利润率 ${formatFinanceMargin(finance.profitMargin)}` : undefined}
                    tone={finance && (finance.costedOrders < finance.settledOrders || (finance.profitMicrocredits ?? 0) < 0) ? "warning" : "neutral"}
                />
                <AnalyticsHealthCard icon={<Workflow className="size-4" />} label="能力任务分布" value={data ? `${data.kpi.textTasks}/${data.kpi.imageTasks}/${data.kpi.videoTasks}/${data.kpi.audioTasks}` : "--"} detail="文本 / 图片 / 视频 / 音频" />
            </section>

            <section id="admin-analytics-analysis" className="admin-analytics-analysis-section">
                {financeUnavailable && <Alert type="warning" showIcon title="后端未返回完整财务统计，缺失金额显示为 --。" />}
                <div className="admin-analytics-analysis-heading">
                    <div>
                        <h2>深度分析</h2>
                        <p>按模型、渠道、用户或异常类型继续核对当前统计范围。</p>
                    </div>
                </div>
                <Tabs
                    activeKey={analysisTab}
                    onChange={(key) => setAnalysisTab(key as AnalysisTab)}
                    className="admin-analytics-tabs"
                    items={[
                        {
                            key: "models",
                            label: "模型分析",
                            children: (
                                <AdminDataTable
                                    table={{ rowKey: (row) => `${row.model}:${row.capability}`, size: "small", loading, columns: modelColumns, dataSource: pageRows(modelRows, modelPage), pagination: false, scroll: { x: 2160 } }}
                                    empty={<AdminTableEmpty />}
                                    skeletonColumns={16}
                                    footer={<PaginationBar alwaysShow current={modelPage} pageSize={analyticsPageSize} total={modelRows.length} onChange={(page) => setModelPage(page)} pageSizeOptions={[analyticsPageSize]} />}
                                />
                            ),
                        },
                        {
                            key: "channels",
                            label: "渠道分析",
                            children: (
                                <AdminDataTable
                                    table={{ rowKey: (row) => row.channelId || "local", size: "small", loading, columns: channelColumns, dataSource: pageRows(channelRows, channelPage), pagination: false, scroll: { x: 1480 } }}
                                    empty={<AdminTableEmpty />}
                                    skeletonColumns={11}
                                    footer={<PaginationBar alwaysShow current={channelPage} pageSize={analyticsPageSize} total={channelRows.length} onChange={(page) => setChannelPage(page)} pageSizeOptions={[analyticsPageSize]} />}
                                />
                            ),
                        },
                        {
                            key: "users",
                            label: "个人统计",
                            children: (
                                <AdminDataTable
                                    table={{
                                        rowKey: "userId",
                                        size: "small",
                                        loading,
                                        columns: userColumns,
                                        dataSource: pageRows(userRows, userPage),
                                        pagination: false,
                                        scroll: { x: 2900 },
                                        expandable: { expandedRowRender: (row) => <AnalyticsUserDetail row={row} />, rowExpandable: (row) => row.daily.length > 0 || row.models.length > 0 || row.channels.length > 0 },
                                    }}
                                    empty={<AdminTableEmpty />}
                                    skeletonColumns={19}
                                    footer={<PaginationBar alwaysShow current={userPage} pageSize={analyticsPageSize} total={userRows.length} onChange={(page) => setUserPage(page)} pageSizeOptions={[analyticsPageSize]} />}
                                />
                            ),
                        },
                        {
                            key: "failures",
                            label: `异常定位${data?.failures.length ? ` (${data.failures.reduce((sum, item) => sum + item.count, 0)})` : ""}`,
                            children: (
                                <AdminDataTable
                                    table={{ rowKey: (row) => `${row.type}:${row.model}`, size: "small", loading, columns: failureColumns, dataSource: pageRows(failureRows, failurePage), pagination: false, scroll: { x: 900 } }}
                                    empty={<AdminTableEmpty />}
                                    skeletonColumns={5}
                                    footer={<PaginationBar alwaysShow current={failurePage} pageSize={analyticsPageSize} total={failureRows.length} onChange={(page) => setFailurePage(page)} pageSizeOptions={[analyticsPageSize]} />}
                                />
                            ),
                        },
                    ]}
                />
            </section>

        </div>
    );
}

function AnalyticsConsumptionCard({ icon, label, value, unit, comparison, footer, footerValue, featured = false }: { icon: ReactNode; label: string; value: ReactNode; unit?: string; comparison?: string; footer: string; footerValue?: string; featured?: boolean }) {
    return (
        <article className={`admin-analytics-consumption-card${featured ? " is-featured" : ""}`}>
            <div className="admin-analytics-consumption-icon" aria-hidden="true">{icon}</div>
            <div className="admin-analytics-consumption-label">{label}</div>
            <div className="admin-analytics-consumption-value">
                <strong>{value}</strong>
                {unit ? <span>{unit}</span> : null}
            </div>
            {comparison ? <div className={`admin-analytics-period-delta${comparison.includes("↓") ? " is-negative" : ""}`}>{comparison}</div> : <div className="admin-analytics-period-delta is-empty">暂无上周期对比</div>}
            <div className="admin-analytics-consumption-footer"><span>{footer}</span>{footerValue ? <span>{footerValue}</span> : null}</div>
        </article>
    );
}

function AnalyticsSecondaryMetric({ icon, label, value }: { icon: ReactNode; label: string; value: string }) {
    return (
        <div className="admin-analytics-secondary-metric">
            <span aria-hidden="true">{icon}</span>
            <div><small>{label}</small><strong>{value}</strong></div>
        </div>
    );
}

function AnalyticsUserUsageMetric({ label, value, emphasized = false }: { label: string; value: string; emphasized?: boolean }) {
    return <div className={`admin-analytics-user-usage-metric${emphasized ? " is-emphasized" : ""}`}><span>{label}</span><strong>{value}</strong></div>;
}

function AnimatedHealthValue({ target, format }: { target: number; format: (value: number) => string }) {
    const display = useCountUp(target, 420, 0);
    return <>{format(display)}</>;
}

function AnalyticsUserDetail({ row }: { row: AdminAnalytics["users"][number] }) {
    const summary = [
        ["统计期间", `${row.activeDays} 个活跃日 · ${row.loginCount} 次登录`],
        ["首次活跃", formatAnalyticsTime(row.firstActiveAt)],
        ["最后活跃", formatAnalyticsTime(row.lastActiveAt)],
        ["任务耗时", `平均 ${formatDuration(row.averageTaskDurationMs)} · P95 ${formatDuration(row.p95TaskDurationMs)}`],
        ["请求耗时", `P95 ${formatDuration(row.p95RequestDurationMs)}`],
        ["媒体产量", `${row.generatedImages} 图 · ${row.generatedVideos} 视频 · ${row.generatedAudio} 音频 · ${row.videoSeconds} 视频秒`],
        ["视频平均积分/秒", formatAverageVideoCredits(row.models, row.videoSeconds)],
        ["Token", row.usageAvailable ? `${formatNumber(row.inputTokens)} 输入 · ${formatNumber(row.outputTokens)} 输出 · ${formatNumber(row.cachedTokens)} 缓存` : "上游未返回 Token 用量"],
        ["实际积分", formatCredits(row.creditsConsumedMicrocredits)],
        ["上游费用", formatCost(row.estimatedCostMicros, row.currency, row.costAvailable)],
        ["创作活动", `${row.agentMessages} 条 Agent 消息 · ${row.canvasDays} 个画布活跃日 · ${row.assets} 个素材 · ${row.resources} 个资源`],
    ] as const;

    return (
        <div className="admin-analytics-user-detail">
            <div className="admin-analytics-user-detail-heading">
                <div>
                    <h3>{row.name}的个人统计</h3>
                    <p>所有指标均受当前日期、模型、渠道和能力筛选约束。</p>
                </div>
                <div className="admin-analytics-user-detail-status">
                    <AdminStatusBadge label={`${row.succeededTasks} 成功任务`} tone="success" />
                    <AdminStatusBadge label={`${row.failedTasks} 失败任务`} tone={row.failedTasks > 0 ? "error" : "neutral"} />
                    <AdminStatusBadge label={`${row.succeededRequests}/${row.requests} 成功请求`} tone={row.failedRequests > 0 ? "warning" : "success"} />
                </div>
            </div>

            <dl className="admin-analytics-user-summary">
                {summary.map(([label, value]) => (
                    <div key={label}>
                        <dt>{label}</dt>
                        <dd>{value}</dd>
                    </div>
                ))}
            </dl>

            <div className="admin-analytics-user-breakdowns">
                <section aria-labelledby={`analytics-user-models-${row.userId}`}>
                    <div className="admin-analytics-user-breakdown-heading">
                        <h4 id={`analytics-user-models-${row.userId}`}>个人模型用量</h4>
                        <span>{row.models.length} 个模型与能力组合</span>
                    </div>
                    <div className="admin-analytics-user-table-scroll">
                        <table>
                            <thead>
                                <tr>
                                    <th>模型</th>
                                    <th>能力</th>
                                    <th>任务</th>
                                    <th>任务成功率</th>
                                    <th>请求</th>
                                    <th>请求成功率</th>
                                    <th>媒体 / 视频秒</th>
                                    <th>视频平均积分/秒</th>
                                    <th>Token（入 / 出 / 缓存）</th>
                                    <th>积分</th>
                                    <th>费用</th>
                                </tr>
                            </thead>
                            <tbody>
                                {row.models.map((item) => (
                                    <tr key={`${item.model}:${item.capability}`}>
                                        <td>{item.model}</td>
                                        <td>{capabilityLabel(item.capability)}</td>
                                        <td>{item.tasks}</td>
                                        <td>{percent(item.taskSuccessRate)}</td>
                                        <td>{item.requests}</td>
                                        <td>{percent(item.requestSuccessRate)}</td>
                                        <td>{`${item.mediaCount} / ${item.videoSeconds}`}</td>
                                        <td>{item.capability === "video" && item.videoSeconds > 0 ? `${formatCreditsValue(item.creditsConsumedMicrocredits / item.videoSeconds)} 积分/秒` : "--"}</td>
                                        <td>{item.usageAvailable ? `${formatNumber(item.inputTokens)} / ${formatNumber(item.outputTokens)} / ${formatNumber(item.cachedTokens)}` : "未返回"}</td>
                                        <td>{formatCredits(item.creditsConsumedMicrocredits)}</td>
                                        <td>{formatCost(item.estimatedCostMicros, item.currency, item.costAvailable)}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </section>

                <section aria-labelledby={`analytics-user-channels-${row.userId}`}>
                    <div className="admin-analytics-user-breakdown-heading">
                        <h4 id={`analytics-user-channels-${row.userId}`}>个人渠道用量</h4>
                        <span>{row.channels.length} 个渠道归属</span>
                    </div>
                    <div className="admin-analytics-user-table-scroll">
                        <table>
                            <thead>
                                <tr>
                                    <th>渠道</th>
                                    <th>任务 / 请求</th>
                                    <th>请求成 / 败</th>
                                    <th>请求成功率</th>
                                    <th>模型数</th>
                                    <th>图 / 视 / 音</th>
                                    <th>视频秒</th>
                                    <th>Token（入 / 出 / 缓存）</th>
                                    <th>积分</th>
                                    <th>费用</th>
                                </tr>
                            </thead>
                            <tbody>
                                {row.channels.map((item) => (
                                    <tr key={item.channelId || "unassigned"}>
                                        <td>
                                            <div>{item.name}</div>
                                            <div className="admin-analytics-user-table-subtext">{item.channelId || "无系统渠道 ID"}</div>
                                        </td>
                                        <td>{`${item.tasks} / ${item.requests}`}</td>
                                        <td>{`${item.succeededRequests} / ${item.failedRequests}`}</td>
                                        <td>{percent(item.requestSuccessRate)}</td>
                                        <td>{item.uniqueModels}</td>
                                        <td>{`${item.generatedImages} / ${item.generatedVideos} / ${item.generatedAudio}`}</td>
                                        <td>{item.videoSeconds}</td>
                                        <td>{item.usageAvailable ? `${formatNumber(item.inputTokens)} / ${formatNumber(item.outputTokens)} / ${formatNumber(item.cachedTokens)}` : "未返回"}</td>
                                        <td>{formatCredits(item.creditsConsumedMicrocredits)}</td>
                                        <td>{formatCost(item.estimatedCostMicros, item.currency, item.costAvailable)}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </section>

                <section aria-labelledby={`analytics-user-daily-${row.userId}`}>
                    <div className="admin-analytics-user-breakdown-heading">
                        <h4 id={`analytics-user-daily-${row.userId}`}>个人每日明细</h4>
                        <span>{row.daily.length} 个有记录的自然日</span>
                    </div>
                    <div className="admin-analytics-user-table-scroll">
                        <table>
                            <thead>
                                <tr>
                                    <th>日期</th>
                                    <th>登录</th>
                                    <th>文 / 图 / 视 / 音</th>
                                    <th>任务（成 / 败 / 取消）</th>
                                    <th>排队 / 运行</th>
                                    <th>请求（成 / 败）</th>
                                    <th>图 / 视 / 音</th>
                                    <th>视频秒</th>
                                    <th>Token（入 / 出 / 缓存）</th>
                                    <th>积分</th>
                                    <th>费用</th>
                                    <th>Agent / 画布</th>
                                    <th>素材 / 资源</th>
                                </tr>
                            </thead>
                            <tbody>
                                {row.daily.map((item) => (
                                    <tr key={item.day}>
                                        <td>{item.day}</td>
                                        <td>{item.loginCount}</td>
                                        <td>{`${item.textTasks} / ${item.imageTasks} / ${item.videoTasks} / ${item.audioTasks}`}</td>
                                        <td>{`${item.tasks}（${item.succeededTasks} / ${item.failedTasks} / ${item.cancelledTasks}）`}</td>
                                        <td>{`${item.queuedTasks} / ${item.runningTasks}`}</td>
                                        <td>{`${item.requests}（${item.succeededRequests} / ${item.failedRequests}）`}</td>
                                        <td>{`${item.generatedImages} / ${item.generatedVideos} / ${item.generatedAudio}`}</td>
                                        <td>{item.videoSeconds}</td>
                                        <td>{item.inputTokens + item.outputTokens + item.cachedTokens > 0 ? `${formatNumber(item.inputTokens)} / ${formatNumber(item.outputTokens)} / ${formatNumber(item.cachedTokens)}` : "--"}</td>
                                        <td>{formatCredits(item.creditsConsumedMicrocredits)}</td>
                                        <td>{formatCost(item.estimatedCostMicros, item.currency, item.costAvailable)}</td>
                                        <td>{`${item.agentMessages} / ${item.canvasActive ? "活跃" : "--"}`}</td>
                                        <td>{`${item.assets} / ${item.resources}`}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </section>
            </div>
        </div>
    );
}

function AnalyticsHealthCard({ icon, label, value, trend, detail, tone = "neutral" }: { icon: ReactNode; label: string; value: ReactNode; trend?: { value: string; tone?: AdminStatusTone }; detail?: string; tone?: AdminStatusTone }) {
    return (
        <article className="admin-analytics-health-card" data-tone={tone}>
            <div className="admin-analytics-health-card-heading">
                <span aria-hidden="true">{icon}</span>
                <span>{label}</span>
            </div>
            <div className="admin-analytics-health-card-value">{value}</div>
            <div className="admin-analytics-health-card-meta">
                {trend ? <AdminStatusBadge label={trend.value} tone={trend.tone || "neutral"} /> : null}
                {detail ? <span>{detail}</span> : null}
            </div>
        </article>
    );
}

function FilterSelect({
    label,
    value,
    onChange,
    options,
    filterOption = true,
    loading,
    onSearch,
}: {
    label: string;
    value?: string;
    onChange: (value?: string) => void;
    options: Array<{ label: string; value: string }>;
    filterOption?: boolean;
    loading?: boolean;
    onSearch?: (value: string) => void;
}) {
    return (
        <div className="admin-analytics-field">
            <span>{label}</span>
            <Select aria-label={`${label}筛选`} allowClear showSearch optionFilterProp="label" filterOption={filterOption} loading={loading} placeholder="全部" value={value} onChange={onChange} onSearch={onSearch} options={options} />
        </div>
    );
}

function capabilityLabel(value: string) {
    return capabilityOptions.find((item) => item.value === value)?.label || "未分类";
}

function capabilityIcon(value: string) {
    const className = "size-3.5";
    if (value === "text") return <MessageSquareText className={className} />;
    if (value === "image") return <Image className={className} />;
    if (value === "video") return <Film className={className} />;
    return <Music2 className={className} />;
}

function percent(value: number) {
    return `${Number(value || 0).toFixed(1)}%`;
}

function formatDuration(value: number) {
    if (!value) return "--";
    return value >= 1000 ? `${(value / 1000).toFixed(1)}s` : `${value}ms`;
}

function formatNumber(value: number) {
    return new Intl.NumberFormat("zh-CN", { notation: value >= 100000 ? "compact" : "standard", maximumFractionDigits: 1 }).format(value);
}

function formatPeriodDelta(current?: number, previous?: number) {
    if (current === undefined || previous === undefined) return undefined;
    if (current === previous) return "较上周期 → 0.0%";
    if (previous === 0) return `较上周期 ↑ ${formatNumber(Math.abs(current))}`;
    const delta = ((current - previous) / Math.abs(previous)) * 100;
    return `较上周期 ${delta > 0 ? "↑" : "↓"} ${Math.abs(delta).toFixed(1)}%`;
}

function formatCountDelta(current?: number, previous?: number) {
    if (current === undefined || previous === undefined) return undefined;
    const delta = current - previous;
    const direction = delta > 0 ? "↑" : delta < 0 ? "↓" : "→";
    const value = previous === 0 ? formatNumber(Math.abs(delta)) : `${Math.abs((delta / previous) * 100).toFixed(1)}%`;
    return { value: `较前一日 ${direction} ${value}`, tone: delta >= 0 ? ("success" as const) : ("warning" as const) };
}

function formatRateDelta(current?: number, previous?: number) {
    if (current === undefined || previous === undefined) return undefined;
    const delta = current - previous;
    const direction = delta > 0 ? "↑" : delta < 0 ? "↓" : "→";
    return { value: `较前一日 ${direction} ${Math.abs(delta).toFixed(1)}pp`, tone: delta >= 0 ? ("success" as const) : ("warning" as const) };
}

function formatCost(micros: number, currency?: string, available?: boolean) {
    return available ? formatMoney(fromMicros(micros), currency || "USD") : "--";
}

function formatAnalyticsTime(value?: string) {
    return value ? dayjs(value).format("YYYY-MM-DD HH:mm") : "--";
}

function formatCredits(microcredits: number) {
    return `${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 6 }).format(fromMicros(microcredits))} 积分`;
}

function formatCreditsValue(microcredits: number) {
    return new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 6 }).format(fromMicros(microcredits));
}

function videoCreditsForRows(rows: Array<{ capability: string; creditsConsumedMicrocredits: number }>) {
    return rows.reduce((sum, row) => sum + (row.capability === "video" ? row.creditsConsumedMicrocredits : 0), 0);
}

function averageVideoCreditsPerSecond(rows: Array<{ capability: string; creditsConsumedMicrocredits: number }>, videoSeconds: number) {
    return videoSeconds > 0 ? videoCreditsForRows(rows) / videoSeconds : undefined;
}

function formatAverageVideoCredits(rows: Array<{ capability: string; creditsConsumedMicrocredits: number }>, videoSeconds: number) {
    const average = averageVideoCreditsPerSecond(rows, videoSeconds);
    return average === undefined ? "--" : `${formatCreditsValue(average)} 积分/秒`;
}

function formatCapabilityShare(microcredits: number, total?: number) {
    return total ? `${((microcredits / total) * 100).toFixed(1)}% 消耗` : "0% 消耗";
}

function formatMoney(value: number, currency = "USD") {
    if (currency === "MIXED") return `${value.toFixed(6)}（混合币种）`;
    try {
        return new Intl.NumberFormat("zh-CN", { style: "currency", currency, minimumFractionDigits: 2, maximumFractionDigits: 6 }).format(value);
    } catch {
        return `${currency} ${value.toFixed(6)}`;
    }
}

function fromMicros(value: number) {
    return value / 1_000_000;
}

function filterDate(value: string | null, fallback: Dayjs) {
    if (!value) return fallback;
    const parsed = dayjs(value);
    return parsed.isValid() ? parsed : fallback;
}

function resolveRangePreset(range: [Dayjs, Dayjs]): RangePreset | undefined {
    const end = dayjs();
    if (!range[1].isSame(end, "day")) return undefined;
    if (range[0].isSame(end.subtract(6, "day"), "day")) return "7d";
    if (range[0].isSame(end.subtract(29, "day"), "day")) return "30d";
    if (range[0].isSame(end.subtract(89, "day"), "day")) return "90d";
    return undefined;
}

function parseRangePreset(value: string | null): RangePreset | undefined {
    return value === "7d" || value === "30d" || value === "90d" ? value : undefined;
}

function initialRangePreset(searchParams: URLSearchParams): RangePreset | undefined {
    const explicit = parseRangePreset(searchParams.get("rangePreset"));
    if (explicit) return explicit;
    if (searchParams.has("from") || searchParams.has("to")) return resolveRangePresetFromQuery(searchParams.get("from"), searchParams.get("to"));
    return "30d";
}

function initialAnalyticsRange(searchParams: URLSearchParams, preset: RangePreset | undefined): [Dayjs, Dayjs] {
    const end = dayjs();
    if (preset) {
        const days = preset === "7d" ? 6 : preset === "30d" ? 29 : 89;
        return [end.subtract(days, "day"), end];
    }
    return [filterDate(searchParams.get("from"), end.subtract(29, "day")), filterDate(searchParams.get("to"), end)];
}

function resolveRangePresetFromQuery(from: string | null, to: string | null): RangePreset | undefined {
    const start = from ? dayjs(from) : null;
    const end = to ? dayjs(to) : null;
    if (!start?.isValid() || !end?.isValid()) return undefined;
    return resolveRangePreset([start, end]);
}
