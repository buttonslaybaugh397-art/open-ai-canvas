import { App, Button, Input, Skeleton } from "antd";
import { AlertTriangle, Archive, BadgeCheck, Circle, Download, ExternalLink, FileUp, History, RefreshCw, RotateCcw, ServerCog, ShieldCheck } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ChangeEvent } from "react";

import { checkSystemUpdate, downloadMigrationArchive, getSystemUpdateStatus, importMigrationArchive, rollbackSystemUpdate, startMigrationExport, startSystemUpdate, type MigrationPhase, type SystemUpdateStatus, type UpdatePhase } from "@/services/api/system-update";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminStatusBadge, SettingsSectionCard } from "../components/admin-ui";

const activePhases = new Set<UpdatePhase>(["checking", "preflight", "backing_up", "pulling", "draining", "migrating", "switching", "verifying", "rolling_back"]);
const activeMigrationPhases = new Set<MigrationPhase>(["validating", "backing_up", "stopping", "packaging", "uploading", "restoring", "starting", "verifying"]);

const phaseLabels: Record<UpdatePhase, string> = {
    idle: "等待检查",
    checking: "正在检查",
    ready: "可更新",
    no_update: "已是最新",
    preflight: "前置检查",
    backing_up: "正在备份",
    pulling: "拉取镜像",
    draining: "停止旧服务",
    migrating: "迁移数据库",
    switching: "切换版本",
    verifying: "健康验证",
    succeeded: "更新成功",
    rolling_back: "正在回退",
    rolled_back: "已回退",
    failed: "更新失败",
    manual_intervention: "需人工介入",
};

const migrationPhaseLabels: Record<MigrationPhase, string> = {
    idle: "等待操作",
    validating: "校验迁移包",
    backing_up: "创建恢复前备份",
    stopping: "停止服务",
    packaging: "打包数据与服务",
    uploading: "接收迁移包",
    restoring: "恢复数据与服务",
    starting: "启动恢复后的服务",
    verifying: "健康验证",
    succeeded: "迁移成功",
    failed: "迁移失败",
    manual_intervention: "需人工介入",
};

export default function SystemUpdatePage() {
    const { message, modal } = App.useApp();
    const [status, setStatus] = useState<SystemUpdateStatus | null>(null);
    const [loading, setLoading] = useState(true);
    const [checking, setChecking] = useState(false);
    const [starting, setStarting] = useState(false);
    const [exporting, setExporting] = useState(false);
    const [importing, setImporting] = useState(false);
    const [reconnecting, setReconnecting] = useState(false);
    const [loadError, setLoadError] = useState("");
    const mountedRef = useRef(true);
    const fileInputRef = useRef<HTMLInputElement>(null);

    const load = useCallback(async (initial = false) => {
        if (initial) setLoading(true);
        try {
            const next = await getSystemUpdateStatus();
            if (!mountedRef.current) return;
            setStatus(next);
            setLoadError("");
            setReconnecting(false);
        } catch (error) {
            if (!mountedRef.current) return;
            const errorMessage = error instanceof Error ? error.message : "读取系统更新状态失败";
            setLoadError(errorMessage);
            if (status && (activePhases.has(status.operation.phase) || activeMigrationPhases.has(status.migration?.operation.phase ?? "idle"))) setReconnecting(true);
        } finally {
            if (mountedRef.current && initial) setLoading(false);
        }
    }, [status]);

    useEffect(() => {
        mountedRef.current = true;
        void load(true);
        return () => {
            mountedRef.current = false;
        };
    }, []); // eslint-disable-line react-hooks/exhaustive-deps

    useEffect(() => {
        if (!status || (!activePhases.has(status.operation.phase) && !activeMigrationPhases.has(status.migration?.operation.phase ?? "idle"))) return;
        const timer = window.setInterval(() => void load(false), 2500);
        return () => window.clearInterval(timer);
    }, [load, status]);

    const migration = status?.migration;
    const migrationPhase = migration?.operation.phase ?? "idle";
    const migrationOperationActive = activeMigrationPhases.has(migrationPhase);
    const operationActive = Boolean(status && (activePhases.has(status.operation.phase) || migrationOperationActive));
    const migrationArchive = migration?.operation.archive ?? migration?.lastExport;
    const downloadableMigrationArchive = migration?.lastExport;
    const blockingCheckFailed = status?.checks.some((check) => check.blocking && check.status === "failed") ?? true;

    const requestCheck = async () => {
        setChecking(true);
        try {
            const next = await checkSystemUpdate();
            setStatus(next);
            message.success(next.updateAvailable ? `发现新版本 ${next.latestRelease?.version}` : "当前已是最新版本");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "检查更新失败");
            await load(false);
        } finally {
            setChecking(false);
        }
    };

    const requestStart = () => {
        if (!status?.latestRelease) return;
        const target = status.latestRelease.version;
        modal.confirm({
            title: `更新到 ${target}？`,
            width: 560,
            content: (
                <div className="space-y-3 text-sm text-foreground/70">
                    <p>系统会先创建并校验数据库 ZIP 与数据目录备份，再停止旧服务、执行迁移并切换镜像。</p>
                    <p>更新期间站点会短暂不可用。任何前置检查失败都不会切换服务；切换后验证失败会自动恢复旧版本和数据库。</p>
                </div>
            ),
            okText: "确认并开始更新",
            cancelText: "取消",
            onOk: async () => {
                setStarting(true);
                try {
                    const next = await startSystemUpdate(target);
                    setStatus(next);
                    message.success("更新任务已启动，页面会自动重连并显示进度");
                } catch (error) {
                    message.error(error instanceof Error ? error.message : "无法开始更新");
                    throw error;
                } finally {
                    setStarting(false);
                }
            },
        });
    };

    const requestRollback = () => {
        if (!status?.rollbackVersion) return;
        let reason = "";
        modal.confirm({
            title: `回退到 ${status.rollbackVersion}？`,
            width: 520,
            content: (
                <div className="space-y-3">
                    <p className="text-sm text-foreground/65">回退会停止当前服务、恢复最近一次已校验数据库备份并重新启动旧镜像。请先确认备份时间与业务影响。</p>
                    <Input.TextArea rows={3} maxLength={300} showCount placeholder="填写回退原因（必填）" onChange={(event) => (reason = event.target.value)} />
                </div>
            ),
            okText: "开始回退",
            okButtonProps: { danger: true },
            cancelText: "取消",
            onOk: async () => {
                if (!reason.trim()) {
                    message.warning("请填写回退原因");
                    throw new Error("rollback reason required");
                }
                const next = await rollbackSystemUpdate(reason.trim());
                setStatus(next);
                message.success("回退任务已启动");
            },
        });
    };

    const requestMigrationExport = async () => {
        setExporting(true);
        try {
            const next = await startMigrationExport();
            setStatus(next);
            message.success("迁移包导出已开始，页面会显示打包进度");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "无法开始导出迁移包");
            await load(false);
        } finally {
            setExporting(false);
        }
    };

    const requestMigrationDownload = async () => {
        if (!downloadableMigrationArchive) {
            message.warning("请先完成迁移包导出");
            return;
        }
        try {
            const result = await downloadMigrationArchive();
            const url = URL.createObjectURL(result.blob);
            const anchor = document.createElement("a");
            anchor.href = url;
            anchor.download = result.filename;
            document.body.appendChild(anchor);
            anchor.click();
            anchor.remove();
            window.setTimeout(() => URL.revokeObjectURL(url), 1000);
            message.success("迁移包下载已开始");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "下载迁移包失败");
        }
    };

    const requestMigrationImport = (event: ChangeEvent<HTMLInputElement>) => {
        const archive = event.target.files?.[0];
        event.target.value = "";
        if (!archive) return;
        const maxBytes = migration?.maxArchiveSize || 20 * 1024 * 1024 * 1024;
        if (archive.size <= 0 || archive.size > maxBytes) {
            message.error("迁移包必须大于 0 且不超过 " + formatBytes(maxBytes));
            return;
        }
        if (!archive.name.toLowerCase().endsWith(".zip")) {
            message.error("请选择 ZIP 迁移包");
            return;
        }
        modal.confirm({
            title: "导入这份迁移包？",
            width: 560,
            content: (
                <div className="space-y-3 text-sm text-foreground/70">
                    <p>导入会替换当前数据、配置和 Docker 服务镜像，站点会短暂不可用。</p>
                    <p>系统会先创建导入前恢复包。迁移包包含敏感环境配置，请确认文件来源可信；导入失败会自动尝试恢复原状态。</p>
                    <p className="admin-monospace">{archive.name} · {formatBytes(archive.size)}</p>
                </div>
            ),
            okText: "确认并导入",
            okButtonProps: { danger: true },
            cancelText: "取消",
            onOk: async () => {
                setImporting(true);
                try {
                    const next = await importMigrationArchive(archive);
                    setStatus(next);
                    message.success("迁移包已接收，页面会显示恢复进度");
                } catch (error) {
                    message.error(error instanceof Error ? error.message : "导入迁移包失败");
                    await load(false);
                    throw error;
                } finally {
                    setImporting(false);
                }
            },
        });
    };

    if (loading) {
        return (
            <AdminPageFrame title="系统更新" description="检查发布版本，并在备份、迁移和健康验证保护下完成在线更新。" scroll>
                <div className="admin-settings-stack admin-system-update"><Skeleton active paragraph={{ rows: 12 }} /></div>
            </AdminPageFrame>
        );
    }

    return (
        <AdminPageFrame
            title="系统更新"
            description="检查 GitHub Release，并在完成备份、迁移和健康验证后切换版本。"
            scroll
            actions={
                <Button icon={<RefreshCw className="size-4" />} loading={checking} disabled={operationActive || !status?.supported} onClick={() => void requestCheck()}>
                    检查更新
                </Button>
            }
        >
            <div className="admin-settings-stack admin-system-update">
                {loadError && !status ? <UpdateAlert tone="error" title="无法读取更新状态" detail={loadError} /> : null}
                {!status?.supported ? <UpdateAlert
                    tone="warning"
                    title="当前部署不支持后台在线更新"
                    detail={status?.migration?.supported ? "当前为本地 SQLite 部署，在线版本切换不可用；下方的数据与服务迁移仍然可用。" : "请先在服务器安装 Host Updater，并重建 backend 容器挂载 Unix Socket。"}
                /> : null}
                {reconnecting ? <UpdateAlert tone="warning" title="服务正在切换，等待重新连接" detail="更新器运行在宿主机，后台页面暂时断线不会中止更新。连接恢复后会继续显示最终结果。" /> : null}
                {status?.operation.phase === "manual_intervention" ? <UpdateAlert
                    tone="error"
                    title={status.operation.rollbackError ? "自动回退未完成，需要人工介入" : "应用已更新，Host Updater 需要人工处理"}
                    detail={status.operation.rollbackError || status.operation.error || "请检查 Host Updater 和容器日志。"}
                /> : null}

                <div className="admin-system-update-grid">
                    <SettingsSectionCard
                        className="admin-system-update-release"
                        layout="stacked"
                        icon={<ServerCog className="size-4" />}
                        title="可用版本"
                        description={status?.updateAvailable ? "发现比当前运行版本更新的发布构建。" : "检查 GitHub Release 与当前运行版本。"}
                        status={<PhaseBadge phase={status?.operation.phase ?? "idle"} />}
                        footer={
                            <>
                                <span className="text-xs leading-5 text-foreground/50">每次在线更新都会重新备份数据库，并校验镜像摘要、迁移结果和服务健康状态。</span>
                                <Button type="primary" loading={starting} disabled={!status?.connected || !status.updateAvailable || operationActive || blockingCheckFailed} onClick={requestStart}>
                                    开始更新
                                </Button>
                            </>
                        }
                    >
                        <div className="admin-system-update-release-body">
                            <div className="admin-system-update-version-row">
                                <strong>{status?.latestRelease?.version || "尚未检查"}</strong>
                                {status?.latestRelease?.prerelease ? <AdminStatusBadge label="预览版" tone="warning" /> : null}
                            </div>
                            <dl className="admin-system-update-facts">
                                <dt>当前版本</dt><dd>{status?.currentVersion || "未识别"}</dd>
                                <dt>部署方式</dt><dd>{status?.deployment || "未知"}</dd>
                                <dt>发布时间</dt><dd>{formatDate(status?.latestRelease?.publishedAt)}</dd>
                                <dt>代码仓库</dt><dd>{status?.repository || "ddcat-ai/open-ai-canvas"}</dd>
                            </dl>
                            {status?.latestRelease ? (
                                <div className="admin-system-update-notes">
                                    <div className="flex items-center justify-between gap-3"><h3>更新日志</h3><a href={status.latestRelease.url} target="_blank" rel="noreferrer">查看 Release <ExternalLink className="size-3" /></a></div>
                                    <pre>{status.latestRelease.body || "本版本未填写更新日志。"}</pre>
                                </div>
                            ) : null}
                        </div>
                    </SettingsSectionCard>

                    <SettingsSectionCard layout="stacked" icon={<ShieldCheck className="size-4" />} title="更新前检查" description="所有阻断项通过后才允许切换服务。" status={{ label: status?.connected ? "更新器已连接" : "更新器未连接", color: status?.connected ? "success" : "error" }}>
                        <div className="admin-system-update-checks">
                            {(status?.checks || []).map((check) => <UpdateCheckRow key={check.key} check={check} />)}
                        </div>
                    </SettingsSectionCard>
                </div>

                <SettingsSectionCard layout="stacked" icon={<History className="size-4" />} title="更新进度" description="操作状态和日志由宿主机持久化，服务重启后仍可读取。" status={<PhaseBadge phase={status?.operation.phase ?? "idle"} />}>
                    <div className="admin-system-update-progress">
                        {status?.operation.error ? <UpdateAlert tone="error" title="本次操作失败" detail={status.operation.error} compact /> : null}
                        {(status?.operation.logs || []).length ? (
                            <ol>
                                {status?.operation.logs.map((entry, index) => (
                                    <li key={`${entry.at}-${index}`}><span className="admin-system-update-dot" data-active={index === status.operation.logs.length - 1} /><div><strong>{phaseLabels[entry.phase]}</strong><p>{entry.message}</p></div><time>{formatTime(entry.at)}</time></li>
                                ))}
                            </ol>
                        ) : <p className="admin-system-update-empty">检查更新后，这里会显示完整操作记录。</p>}
                    </div>
                </SettingsSectionCard>

                <SettingsSectionCard
                    className="admin-system-migration"
                    layout="stacked"
                    icon={<Archive className="size-4" />}
                    title="数据与服务迁移"
                    description={migration?.supported ? "打包或恢复业务数据、配置和当前已构建的服务镜像。" : "当前部署未提供可用的数据迁移助手。"}
                    status={<MigrationPhaseBadge phase={migrationPhase} />}
                    footer={
                        <>
                            <span className="text-xs leading-5 text-foreground/50">迁移包包含业务数据、密钥和部署配置；操作会短暂停止服务，导入前会创建恢复点，失败时自动尝试恢复。</span>
                            <Button type="primary" icon={<Archive className="size-4" />} loading={exporting} disabled={!migration?.supported || operationActive} onClick={() => void requestMigrationExport()}>
                                导出迁移包
                            </Button>
                        </>
                    }
                >
                    {!migration?.supported ? (
                        <div className="admin-system-migration-unavailable">
                            <p>{migration?.reason || "请检查本地迁移助手或服务器 Host Updater 的连接状态。"}</p>
                        </div>
                    ) : (
                        <>
                            <div className="admin-system-migration-toolbar">
                                <div>
                                    <strong>{migration.operation.kind === "import" ? "导入迁移包" : migration.operation.kind === "export" ? "导出迁移包" : "迁移包管理"}</strong>
                                    <p>{migrationArchive ? "最近一次有效迁移包已通过结构和 SHA-256 校验。" : "单个迁移包上限：" + formatBytes(migration.maxArchiveSize)}</p>
                                </div>
                                <div className="admin-system-migration-actions">
                                    <input ref={fileInputRef} type="file" accept=".zip,application/zip" onChange={requestMigrationImport} />
                                    <Button icon={<Download className="size-4" />} disabled={!downloadableMigrationArchive || operationActive} onClick={() => void requestMigrationDownload()}>
                                        下载迁移包
                                    </Button>
                                    <Button icon={<FileUp className="size-4" />} loading={importing} disabled={operationActive} onClick={() => fileInputRef.current?.click()}>
                                        导入迁移包
                                    </Button>
                                </div>
                            </div>
                            {migrationArchive ? (
                                <dl className="admin-system-migration-facts">
                                    <dt>迁移包编号</dt><dd>{migrationArchive.id}</dd>
                                    <dt>来源版本</dt><dd>{migrationArchive.version || "未记录"}</dd>
                                    <dt>数据库</dt><dd>{migrationArchive.databaseDriver === "sqlite" ? "SQLite" : migrationArchive.databaseDriver === "postgres" ? "PostgreSQL" : migrationArchive.databaseDriver || "未记录"}</dd>
                                    <dt>文件大小</dt><dd>{formatBytes(migrationArchive.size)}</dd>
                                    <dt>生成时间</dt><dd>{formatDate(migrationArchive.createdAt)}</dd>
                                    <dt>SHA-256</dt><dd className="admin-monospace">{migrationArchive.checksum || "未返回"}</dd>
                                </dl>
                            ) : null}
                            {migration.operation.error ? <UpdateAlert tone="error" title="本次迁移失败" detail={migration.operation.error} compact /> : null}
                            {(migration.operation.logs || []).length ? (
                                <div className="admin-system-migration-log">
                                    <h3>迁移日志</h3>
                                    <ol>
                                        {migration.operation.logs.map((entry, index) => (
                                            <li key={entry.at + "-" + index}><span className="admin-system-update-dot" data-active={index === migration.operation.logs.length - 1} /><div><strong>{migrationPhaseLabels[entry.phase]}</strong><p>{entry.message}</p></div><time>{formatTime(entry.at)}</time></li>
                                        ))}
                                    </ol>
                                </div>
                            ) : <p className="admin-system-update-empty">导出或导入迁移包后，这里会显示完整操作记录。</p>}
                        </>
                    )}
                </SettingsSectionCard>

                <SettingsSectionCard
                    layout="stacked"
                    icon={<Archive className="size-4" />}
                    title="备份与回退"
                    description="只使用最近一次通过 ZIP 结构与 SHA-256 校验的备份。"
                    status={{ label: status?.lastBackup ? "备份已校验" : "暂无备份", color: status?.lastBackup ? "success" : "warning" }}
                    footer={
                        <>
                            <span className="text-xs text-foreground/50">人工回退同样会执行停服、数据库恢复、旧镜像启动和健康验证。</span>
                            <Button danger icon={<RotateCcw className="size-4" />} disabled={!status?.rollbackVersion || operationActive || !status?.connected} onClick={requestRollback}>回退到上一版本</Button>
                        </>
                    }
                >
                    <div className="admin-system-update-backup">
                        {status?.lastBackup ? <dl><dt>备份编号</dt><dd>{status.lastBackup.id}</dd><dt>来源版本</dt><dd>{status.lastBackup.version}</dd><dt>创建时间</dt><dd>{formatDate(status.lastBackup.createdAt)}</dd><dt>文件大小</dt><dd>{formatBytes(status.lastBackup.size)}</dd><dt>SHA-256</dt><dd className="admin-monospace">{status.lastBackup.checksum}</dd></dl> : <p>首次开始更新时会自动创建数据库与数据目录 ZIP；备份失败将直接中止更新。</p>}
                    </div>
                </SettingsSectionCard>
            </div>
        </AdminPageFrame>
    );
}

function PhaseBadge({ phase }: { phase: UpdatePhase }) {
    const tone = phase === "succeeded" || phase === "ready" ? "success" : phase === "failed" || phase === "manual_intervention" ? "error" : phase === "rolling_back" || phase === "rolled_back" ? "warning" : activePhases.has(phase) ? "info" : "neutral";
    return <AdminStatusBadge label={phaseLabels[phase]} tone={tone} />;
}

function MigrationPhaseBadge({ phase }: { phase: MigrationPhase }) {
    const tone = phase === "succeeded" ? "success" : phase === "failed" || phase === "manual_intervention" ? "error" : activeMigrationPhases.has(phase) ? "info" : "neutral";
    return <AdminStatusBadge label={migrationPhaseLabels[phase]} tone={tone} />;
}

function UpdateCheckRow({ check }: { check: SystemUpdateStatus["checks"][number] }) {
    const Icon = check.status === "passed" ? BadgeCheck : check.status === "failed" ? AlertTriangle : Circle;
    return <div className="admin-system-update-check-row" data-status={check.status}><Icon className="size-4" /><div><strong>{check.label}</strong><p>{check.detail || "等待执行"}</p></div><span>{check.status === "passed" ? "通过" : check.status === "failed" ? "失败" : "待执行"}</span></div>;
}

function UpdateAlert({ tone, title, detail, compact = false }: { tone: "warning" | "error"; title: string; detail: string; compact?: boolean }) {
    return <div className={`admin-system-update-alert${compact ? " is-compact" : ""}`} data-tone={tone} role="alert"><AlertTriangle className="size-4" /><div><strong>{title}</strong><p>{detail}</p></div></div>;
}

function formatDate(value?: string) {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString("zh-CN", { hour12: false });
}

function formatTime(value: string) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "—" : date.toLocaleTimeString("zh-CN", { hour12: false });
}

function formatBytes(value: number) {
    if (!Number.isFinite(value) || value <= 0) return "—";
    const units = ["B", "KB", "MB", "GB", "TB"];
    const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
    return `${(value / 1024 ** index).toFixed(index > 1 ? 2 : 0)} ${units[index]}`;
}
