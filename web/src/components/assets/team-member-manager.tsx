import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Avatar, Button, Drawer, Empty, Form, Input, InputNumber, Pagination, Popconfirm, Progress, Select, Skeleton, Tag, Typography } from "antd";
import { Database, History, Images, LogOut, Save, UserPlus, Users } from "lucide-react";

import { addTeamMember, getTeam, listTeamAuditEvents, listTeamMembers, removeTeamMember, updateTeam, updateTeamMemberRole, type TeamAuditEvent, type TeamMember, type TeamRole, type TeamSummary } from "@/services/api/team-assets";
import { useUserStore } from "@/stores/use-user-store";

const roleLabels: Record<TeamRole, string> = { owner: "所有者", admin: "管理员", editor: "编辑者", viewer: "查看者" };
const GIGABYTE = 1024 ** 3;
type TeamSettingsFields = { name: string; description: string; assetLimit: number; storageLimitGB: number };

export function TeamMemberManager({ open, team, onClose, onLeftTeam }: { open: boolean; team?: TeamSummary; onClose: () => void; onLeftTeam: (teamId: string) => void }) {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const [settingsForm] = Form.useForm<TeamSettingsFields>();
    const currentUserId = useUserStore((state) => state.user?.id || "");
    const [username, setUsername] = useState("");
    const [role, setRole] = useState<Exclude<TeamRole, "owner">>("viewer");
    const [auditPage, setAuditPage] = useState(1);
    const teamId = team?.id || "";
    const canManage = team?.role === "owner" || team?.role === "admin";
    const canEditSettings = team?.role === "owner";
    const assignableRoles: Array<{ label: string; value: Exclude<TeamRole, "owner"> }> = team?.role === "owner"
        ? [{ label: "管理员", value: "admin" }, { label: "编辑者", value: "editor" }, { label: "查看者", value: "viewer" }]
        : [{ label: "编辑者", value: "editor" }, { label: "查看者", value: "viewer" }];

    const membersQuery = useQuery({
        queryKey: ["team-members", teamId],
        queryFn: ({ signal }) => listTeamMembers(teamId, signal),
        enabled: open && Boolean(teamId),
    });
    const detailQuery = useQuery({
        queryKey: ["team-detail", teamId],
        queryFn: ({ signal }) => getTeam(teamId, signal),
        enabled: open && Boolean(teamId),
    });
    const auditQuery = useQuery({
        queryKey: ["team-audit-events", teamId, auditPage],
        queryFn: ({ signal }) => listTeamAuditEvents(teamId, auditPage, 10, signal),
        enabled: open && Boolean(teamId) && canManage,
    });
    useEffect(() => setAuditPage(1), [teamId, open]);
    useEffect(() => {
        if (!detailQuery.data) return;
        settingsForm.setFieldsValue({
            name: detailQuery.data.team.name,
            description: detailQuery.data.team.description || "",
            assetLimit: detailQuery.data.usage.assetLimit,
            storageLimitGB: bytesToGigabytes(detailQuery.data.usage.storageLimitBytes),
        });
    }, [detailQuery.data, settingsForm]);
    const refreshMembers = async () => {
        await Promise.all([
            queryClient.invalidateQueries({ queryKey: ["team-members", teamId] }),
            queryClient.invalidateQueries({ queryKey: ["team-detail", teamId] }),
        ]);
    };
    const settingsMutation = useMutation({
        mutationFn: (values: TeamSettingsFields) => updateTeam(teamId, {
            name: values.name.trim(),
            description: values.description.trim(),
            assetLimit: Math.trunc(values.assetLimit),
            storageLimitBytes: Math.round(values.storageLimitGB * GIGABYTE),
        }),
        onSuccess: async (detail) => {
            queryClient.setQueryData(["team-detail", teamId], detail);
            await Promise.all([
                queryClient.invalidateQueries({ queryKey: ["teams"] }),
                queryClient.invalidateQueries({ queryKey: ["team-asset-picker-teams"] }),
            ]);
            message.success("团队设置已保存");
        },
        onError: (error) => message.error(readMemberError(error, "保存团队设置失败")),
    });
    const addMutation = useMutation({
        mutationFn: () => addTeamMember(teamId, { username: username.trim(), role }),
        onSuccess: async () => {
            setUsername("");
            setRole("viewer");
            await refreshMembers();
            message.success("成员已加入团队");
        },
        onError: (error) => message.error(readMemberError(error, "添加成员失败")),
    });
    const roleMutation = useMutation({
        mutationFn: ({ userId, nextRole }: { userId: string; nextRole: Exclude<TeamRole, "owner"> }) => updateTeamMemberRole(teamId, userId, nextRole),
        onSuccess: async () => {
            await refreshMembers();
            message.success("成员角色已更新");
        },
        onError: (error) => message.error(readMemberError(error, "更新成员角色失败")),
    });
    const removeMutation = useMutation({
        mutationFn: (userId: string) => removeTeamMember(teamId, userId),
        onSuccess: async ({ userId }) => {
            if (userId === currentUserId) {
                await queryClient.invalidateQueries({ queryKey: ["teams"] });
                message.success("已退出团队");
                onLeftTeam(teamId);
                return;
            }
            await refreshMembers();
            message.success("成员已移出团队");
        },
        onError: (error) => message.error(readMemberError(error, "移除成员失败")),
    });

    return <Drawer
        className="library-drawer"
        title={<div><div>{detailQuery.data?.team.name || team?.name || "团队"} · 团队中心</div><Typography.Text type="secondary" className="text-xs font-normal">集中查看使用量、配额和成员权限</Typography.Text></div>}
        open={open}
        size="large"
        onClose={onClose}
        destroyOnHidden
    >
        {detailQuery.isLoading ? <Skeleton active paragraph={{ rows: 3 }} /> : detailQuery.isError ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={readMemberError(detailQuery.error, "团队信息加载失败")}><Button onClick={() => void detailQuery.refetch()}>重新加载</Button></Empty>
        ) : detailQuery.data ? <>
            <UsageOverview usage={detailQuery.data.usage} />
            <section className="mb-5 rounded-[var(--r-lg)] border border-border bg-surface-subtle p-4">
                <div className="mb-3 flex items-center justify-between gap-3">
                    <div>
                        <div className="text-sm font-semibold">团队设置</div>
                        <Typography.Text type="secondary" className="text-xs">{canEditSettings ? "配额不能低于当前使用量" : "仅团队所有者可以修改"}</Typography.Text>
                    </div>
                    {!canEditSettings ? <Tag className="m-0">只读</Tag> : null}
                </div>
                <Form form={settingsForm} layout="vertical" disabled={!canEditSettings} onFinish={(values) => settingsMutation.mutate(values)}>
                    <Form.Item name="name" label="团队名称" rules={[{ required: true, message: "请输入团队名称" }, { max: 120, message: "不能超过 120 个字符" }]}>
                        <Input maxLength={120} />
                    </Form.Item>
                    <Form.Item name="description" label="团队描述" rules={[{ max: 500, message: "不能超过 500 个字符" }]}>
                        <Input.TextArea autoSize={{ minRows: 2, maxRows: 5 }} maxLength={500} showCount placeholder="说明团队用途、素材规范或协作约定" />
                    </Form.Item>
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                        <Form.Item name="assetLimit" label="素材数量上限" rules={[{ required: true, message: "请输入素材上限" }]}>
                            <InputNumber className="w-full" min={1} max={1_000_000} precision={0} addonAfter="个" />
                        </Form.Item>
                        <Form.Item name="storageLimitGB" label="存储空间上限" rules={[{ required: true, message: "请输入存储上限" }]}>
                            <InputNumber className="w-full" min={1} max={10 * 1024} precision={2} addonAfter="GB" />
                        </Form.Item>
                    </div>
                    {canEditSettings ? <Button type="primary" htmlType="submit" icon={<Save className="size-3.5" />} loading={settingsMutation.isPending}>保存设置</Button> : null}
                </Form>
            </section>
        </> : null}

        {canManage ? <section className="mb-5 rounded-[var(--r-lg)] border border-border bg-surface-subtle p-4">
            <div className="mb-3 flex items-center gap-2 text-sm font-semibold"><UserPlus className="size-4" />添加已有用户</div>
            <div className="flex flex-col gap-2 sm:flex-row">
                <Input value={username} maxLength={80} placeholder="输入准确用户名" onChange={(event) => setUsername(event.target.value)} onPressEnter={() => { if (username.trim()) addMutation.mutate(); }} />
                <Select className="w-full sm:w-32" value={role} options={assignableRoles} onChange={setRole} />
                <Button type="primary" loading={addMutation.isPending} disabled={!username.trim()} onClick={() => addMutation.mutate()}>添加</Button>
            </div>
            <Typography.Text type="secondary" className="mt-2 block text-xs">用户需要先注册影策账号；管理员不能任命其他管理员。</Typography.Text>
        </section> : null}

        {membersQuery.isLoading ? <Skeleton active paragraph={{ rows: 5 }} /> : membersQuery.isError ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={readMemberError(membersQuery.error, "成员列表加载失败")}><Button onClick={() => void membersQuery.refetch()}>重新加载</Button></Empty>
        ) : <div className="space-y-2">
            {(membersQuery.data?.members || []).map((member) => <MemberRow
                key={member.userId}
                member={member}
                actorRole={team?.role}
                currentUserId={currentUserId}
                assignableRoles={assignableRoles}
                changing={roleMutation.isPending && roleMutation.variables?.userId === member.userId}
                removing={removeMutation.isPending && removeMutation.variables === member.userId}
                onRoleChange={(nextRole) => roleMutation.mutate({ userId: member.userId, nextRole })}
                onRemove={() => removeMutation.mutate(member.userId)}
            />)}
            {!membersQuery.data?.members.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无团队成员" /> : null}
        </div>}

        {canManage ? <section className="mt-5 border-t border-border pt-5">
            <div className="mb-3 flex items-center justify-between gap-3">
                <div>
                    <div className="flex items-center gap-2 text-sm font-semibold"><History className="size-4" />操作记录</div>
                    <Typography.Text type="secondary" className="text-xs">仅所有者和管理员可见，不记录素材正文或资源地址</Typography.Text>
                </div>
                <Button size="small" onClick={() => void auditQuery.refetch()} loading={auditQuery.isFetching}>刷新</Button>
            </div>
            {auditQuery.isLoading ? <Skeleton active paragraph={{ rows: 4 }} /> : auditQuery.isError ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={readMemberError(auditQuery.error, "操作记录加载失败")}><Button onClick={() => void auditQuery.refetch()}>重新加载</Button></Empty>
            ) : <>
                <div className="space-y-2">
                    {(auditQuery.data?.events || []).map((event) => <AuditEventRow key={event.id} event={event} />)}
                    {!auditQuery.data?.events.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无操作记录" /> : null}
                </div>
                {(auditQuery.data?.total || 0) > 10 ? <div className="mt-4 flex justify-end"><Pagination current={auditPage} pageSize={10} total={auditQuery.data?.total || 0} showSizeChanger={false} onChange={setAuditPage} /></div> : null}
            </>}
        </section> : null}
    </Drawer>;
}

function AuditEventRow({ event }: { event: TeamAuditEvent }) {
    const actor = event.actorDisplayName || event.actorUsername || "已删除用户";
    return <div className="flex items-start gap-3 rounded-[var(--r-lg)] border border-border px-3 py-3">
        <Avatar size="small" className="mt-0.5 shrink-0 bg-[var(--workspace-accent)]">{actor.slice(0, 1).toUpperCase()}</Avatar>
        <div className="min-w-0 flex-1">
            <div className="text-sm"><span className="font-medium">{actor}</span><span className="ml-2 text-foreground/70">{event.summary}</span></div>
            <div className="mt-1 text-xs text-foreground/45">{formatAuditDate(event.createdAt)}{event.targetId ? " · " + event.targetId : ""}</div>
        </div>
    </div>;
}

function formatAuditDate(value: string) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "未知时间" : date.toLocaleString("zh-CN", { hour12: false });
}

function UsageOverview({ usage }: { usage: { memberCount: number; assetCount: number; assetLimit: number; storageBytes: number; storageLimitBytes: number } }) {
    const assetPercent = quotaPercent(usage.assetCount, usage.assetLimit);
    const storagePercent = quotaPercent(usage.storageBytes, usage.storageLimitBytes);
    return <section className="mb-5">
        <div className="mb-3 grid grid-cols-1 gap-3 sm:grid-cols-3">
            <UsageMetric icon={<Users className="size-4" />} label="活跃成员" value={`${usage.memberCount}`} suffix="人" />
            <UsageMetric icon={<Images className="size-4" />} label="团队素材" value={`${usage.assetCount}`} suffix={`/ ${usage.assetLimit} 个`} />
            <UsageMetric icon={<Database className="size-4" />} label="唯一资源存储" value={formatBytes(usage.storageBytes)} suffix={`/ ${formatBytes(usage.storageLimitBytes)}`} />
        </div>
        <div className="space-y-3 rounded-[var(--r-lg)] border border-border p-4">
            <QuotaProgress label="素材数量" percent={assetPercent} detail={`${usage.assetCount} / ${usage.assetLimit}`} />
            <QuotaProgress label="存储空间" percent={storagePercent} detail={`${formatBytes(usage.storageBytes)} / ${formatBytes(usage.storageLimitBytes)}`} />
        </div>
    </section>;
}

function UsageMetric({ icon, label, value, suffix }: { icon: ReactNode; label: string; value: string; suffix: string }) {
    return <div className="rounded-[var(--r-lg)] border border-border bg-surface-subtle p-4">
        <div className="mb-2 flex items-center gap-2 text-xs text-foreground/50">{icon}{label}</div>
        <div className="font-semibold tabular-nums"><span className="text-lg">{value}</span><span className="ml-1 text-xs font-normal text-foreground/45">{suffix}</span></div>
    </div>;
}

function QuotaProgress({ label, percent, detail }: { label: string; percent: number; detail: string }) {
    return <div>
        <div className="mb-1 flex items-center justify-between gap-3 text-xs"><span>{label}</span><span className="tabular-nums text-foreground/50">{detail}</span></div>
        <Progress percent={percent} showInfo={false} size="small" status={percent >= 100 ? "exception" : "normal"} />
    </div>;
}

function quotaPercent(value: number, limit: number) {
    if (limit <= 0) return 100;
    return Math.min(100, Math.round((value / limit) * 100));
}

function bytesToGigabytes(bytes: number) {
    return Math.round((bytes / GIGABYTE) * 100) / 100;
}

function formatBytes(bytes: number) {
    if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    const index = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
    const value = bytes / 1024 ** index;
    return `${value >= 100 || index === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`;
}

function MemberRow({ member, actorRole, currentUserId, assignableRoles, changing, removing, onRoleChange, onRemove }: { member: TeamMember; actorRole?: TeamRole; currentUserId: string; assignableRoles: Array<{ label: string; value: Exclude<TeamRole, "owner"> }>; changing: boolean; removing: boolean; onRoleChange: (role: Exclude<TeamRole, "owner">) => void; onRemove: () => void }) {
    const isSelf = member.userId === currentUserId;
    const actorCanManageTarget = !isSelf && member.role !== "owner" && (actorRole === "owner" || (actorRole === "admin" && member.role !== "admin"));
    const canLeave = isSelf && member.role !== "owner";
    const joinedAt = formatMemberDate(member.joinedAt);

    return <div className="flex flex-col gap-3 rounded-[var(--r-lg)] border border-border px-3 py-3 sm:flex-row sm:items-center">
        <Avatar className="shrink-0 bg-[var(--workspace-accent)]">{(member.displayName || member.username).slice(0, 1).toUpperCase()}</Avatar>
        <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-center gap-2"><span className="truncate font-medium">{member.displayName || member.username}</span>{isSelf ? <Tag className="m-0">我</Tag> : null}</div>
            <div className="mt-0.5 truncate text-xs text-foreground/45">@{member.username} · {joinedAt} 加入</div>
        </div>
        <div className="flex items-center gap-2">
            {actorCanManageTarget ? <Select
                className="w-28"
                value={member.role as Exclude<TeamRole, "owner">}
                options={assignableRoles}
                loading={changing}
                disabled={changing || removing}
                onChange={onRoleChange}
            /> : <Tag className="m-0" color={member.role === "owner" ? "gold" : member.role === "admin" ? "blue" : undefined}>{roleLabels[member.role]}</Tag>}
            {actorCanManageTarget ? <Popconfirm title={`移除 ${member.displayName || member.username}？`} description="移除后将立即失去团队素材访问权，已复制到个人素材的副本不受影响。" okText="移除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={onRemove}><Button danger type="text" loading={removing}>移除</Button></Popconfirm> : null}
            {canLeave ? <Popconfirm title="退出这个团队？" description="退出后将无法继续访问团队素材，个人素材副本仍会保留。" okText="退出" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={onRemove}><Button danger type="text" icon={<LogOut className="size-3.5" />} loading={removing}>退出</Button></Popconfirm> : null}
        </div>
    </div>;
}

function formatMemberDate(value: string) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "未知时间" : date.toLocaleDateString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" });
}

function readMemberError(error: unknown, fallback: string) {
    return error instanceof Error && error.message ? error.message : fallback;
}
