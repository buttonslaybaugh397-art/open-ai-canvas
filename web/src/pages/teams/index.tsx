import { useEffect, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Avatar, Button, Empty, Form, Input, Modal, Popconfirm, Progress, Select, Skeleton, Tag } from "antd";
import { ArrowRight, CalendarClock, Copy, Database, History, Images, Link2, Plus, Settings2, ShieldCheck, Sparkles, UserPlus, UsersRound } from "lucide-react";
import { useNavigate } from "react-router";

import { TeamMemberManager } from "@/components/assets/team-member-manager";
import { PageHeader, WorkspacePage } from "@/components/layout/workspace-page";
import { WorkspaceState } from "@/components/layout/workspace-state";
import { useCopyText } from "@/hooks/use-copy-text";
import { cn } from "@/lib/utils";
import { createTeam, createTeamInvitation, getTeam, listTeamAssets, listTeamAuditEvents, listTeamInvitations, listTeamMembers, listTeams, revokeTeamInvitation, type TeamInvitation, type TeamRole } from "@/services/api/team-assets";

const roleLabels: Record<TeamRole, string> = { owner: "所有者", admin: "管理员", editor: "编辑者", viewer: "查看者" };
type InviteRole = Exclude<TeamRole, "owner">;
type InviteHours = 24 | 72 | 168 | 720;

export default function TeamsPage() {
    const { message } = App.useApp();
    const navigate = useNavigate();
    const copyText = useCopyText();
    const queryClient = useQueryClient();
    const [activeTeamId, setActiveTeamId] = useState("");
    const [createOpen, setCreateOpen] = useState(false);
    const [manageOpen, setManageOpen] = useState(false);
    const [inviteOpen, setInviteOpen] = useState(false);
    const [teamName, setTeamName] = useState("");
    const [inviteRole, setInviteRole] = useState<InviteRole>("editor");
    const [inviteHours, setInviteHours] = useState<InviteHours>(72);
    const [createdInviteUrl, setCreatedInviteUrl] = useState("");

    const teamsQuery = useQuery({ queryKey: ["teams"], queryFn: ({ signal }) => listTeams(signal), staleTime: 30_000 });
    const teams = teamsQuery.data?.teams || [];
    const activeTeam = teams.find((team) => team.id === activeTeamId) || teams[0];
    const teamId = activeTeam?.id || "";
    const canManage = activeTeam?.role === "owner" || activeTeam?.role === "admin";

    useEffect(() => {
        if (activeTeamId && teams.some((team) => team.id === activeTeamId)) return;
        setActiveTeamId(teams[0]?.id || "");
    }, [activeTeamId, teams]);

    const detailQuery = useQuery({ queryKey: ["team-detail", teamId], queryFn: ({ signal }) => getTeam(teamId, signal), enabled: Boolean(teamId) });
    const membersQuery = useQuery({ queryKey: ["team-members", teamId], queryFn: ({ signal }) => listTeamMembers(teamId, signal), enabled: Boolean(teamId) });
    const assetsQuery = useQuery({ queryKey: ["team-dashboard-assets", teamId], queryFn: ({ signal }) => listTeamAssets(teamId, { page: 1, pageSize: 6, signal }), enabled: Boolean(teamId) });
    const auditQuery = useQuery({ queryKey: ["team-audit-events", teamId, 1], queryFn: ({ signal }) => listTeamAuditEvents(teamId, 1, 6, signal), enabled: Boolean(teamId && canManage) });
    const invitationsQuery = useQuery({ queryKey: ["team-invitations", teamId], queryFn: ({ signal }) => listTeamInvitations(teamId, signal), enabled: Boolean(teamId && canManage) });

    const createMutation = useMutation({
        mutationFn: () => createTeam({ name: teamName.trim() }),
        onSuccess: async ({ team }) => {
            await queryClient.invalidateQueries({ queryKey: ["teams"] });
            setActiveTeamId(team.id);
            setTeamName("");
            setCreateOpen(false);
            message.success("团队工作区已创建");
        },
        onError: (error) => message.error(readError(error, "创建团队失败")),
    });
    const inviteMutation = useMutation({
        mutationFn: () => createTeamInvitation(teamId, { role: inviteRole, validHours: inviteHours }),
        onSuccess: async ({ inviteUrl }) => {
            setCreatedInviteUrl(new URL(inviteUrl, window.location.origin).toString());
            await Promise.all([queryClient.invalidateQueries({ queryKey: ["team-invitations", teamId] }), queryClient.invalidateQueries({ queryKey: ["team-audit-events", teamId] })]);
            message.success("邀请链接已创建，仅此处显示完整链接");
        },
        onError: (error) => message.error(readError(error, "创建邀请失败")),
    });
    const revokeMutation = useMutation({
        mutationFn: (invitationId: string) => revokeTeamInvitation(teamId, invitationId),
        onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ["team-invitations", teamId] }); message.success("邀请已撤销"); },
        onError: (error) => message.error(readError(error, "撤销邀请失败")),
    });

    if (teamsQuery.isLoading) return <WorkspacePage grid><WorkspaceState icon="assets" title="正在打开团队工作区" description="读取你的团队、角色和协作数据。" /></WorkspacePage>;
    if (teamsQuery.isError) return <WorkspacePage grid><WorkspaceState icon="assets" title="团队加载失败" description={readError(teamsQuery.error, "请稍后重试")} action={<Button onClick={() => void teamsQuery.refetch()}>重新加载</Button>} /></WorkspacePage>;

    const openAssets = () => navigate("/assets?scope=team&teamId=" + encodeURIComponent(teamId));
    const openInvite = () => { setCreatedInviteUrl(""); setInviteOpen(true); };

    return <>
        <WorkspacePage grid className="teams-workspace-page">
            <PageHeader title="团队工作区" description="把成员、共享素材、用量和协作记录放在同一个工作面。" meta={<span className="app-projects-header-meta">{teams.length} 个团队</span>} actions={<Button type="primary" icon={<Plus className="size-4" />} onClick={() => setCreateOpen(true)}>创建团队</Button>} />
            {!activeTeam ? <FirstTeam onCreate={() => setCreateOpen(true)} /> : <div className="mt-5 grid min-w-0 gap-4 xl:grid-cols-[260px_minmax(0,1fr)]">
                <aside className="min-w-0 rounded-[var(--r-lg)] border border-border bg-surface p-3 xl:sticky xl:top-3 xl:self-start">
                    <div className="mb-2 px-2 text-xs font-medium text-foreground/45">我的团队</div>
                    <div className="space-y-1">{teams.map((team) => <button key={team.id} type="button" className={cn("flex w-full items-center gap-3 rounded-[var(--r-md)] px-3 py-3 text-left transition-colors", team.id === activeTeam.id ? "bg-foreground/[.07]" : "hover:bg-surface-hover")} onClick={() => setActiveTeamId(team.id)}><Avatar shape="square" className="shrink-0 bg-[var(--workspace-accent)]">{team.name.slice(0, 1)}</Avatar><span className="min-w-0 flex-1"><span className="block truncate text-sm font-medium">{team.name}</span><span className="mt-0.5 block text-xs text-foreground/45">{roleLabels[team.role]}</span></span></button>)}</div>
                    <Button className="mt-3 w-full" type="dashed" icon={<Plus className="size-3.5" />} onClick={() => setCreateOpen(true)}>新建团队</Button>
                </aside>
                <div className="min-w-0 space-y-4">
                    <section className="relative overflow-hidden rounded-[var(--r-xl)] border border-border bg-surface p-5 sm:p-7">
                        <div className="pointer-events-none absolute -right-20 -top-24 size-72 rounded-full bg-[color-mix(in_srgb,var(--workspace-accent)_16%,transparent)] blur-3xl" />
                        <div className="relative flex flex-col justify-between gap-5 lg:flex-row lg:items-end"><div className="min-w-0"><div className="mb-3 flex items-center gap-2"><Tag color={activeTeam.role === "owner" ? "gold" : activeTeam.role === "admin" ? "blue" : undefined}>{roleLabels[activeTeam.role]}</Tag>{canManage ? <span className="flex items-center gap-1 text-xs text-foreground/45"><ShieldCheck className="size-3.5" />可管理协作</span> : null}</div><h2 className="truncate text-2xl font-semibold tracking-tight sm:text-3xl">{activeTeam.name}</h2><p className="mt-2 max-w-2xl text-sm leading-6 text-foreground/55">{detailQuery.data?.team.description || "这是团队的共同工作面。添加成员、共享第一份素材，让协作真正开始流动。"}</p></div><div className="flex flex-wrap gap-2"><Button icon={<Images className="size-4" />} onClick={openAssets}>打开共享素材</Button>{canManage ? <Button type="primary" icon={<UserPlus className="size-4" />} onClick={openInvite}>邀请成员</Button> : null}<Button icon={<Settings2 className="size-4" />} onClick={() => setManageOpen(true)}>成员与设置</Button></div></div>
                    </section>
                    {detailQuery.isLoading ? <Skeleton active /> : detailQuery.data ? <UsageCards usage={detailQuery.data.usage} /> : null}
                    <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,1.2fr)_minmax(280px,.8fr)]">
                        <section className="min-w-0 rounded-[var(--r-lg)] border border-border bg-surface p-5"><SectionTitle icon={<Images className="size-4" />} title="共享素材" detail={(assetsQuery.data?.total || 0) + " 项"} action={<Button type="text" onClick={openAssets}>查看全部 <ArrowRight className="ml-1 inline size-3.5" /></Button>} />{assetsQuery.isLoading ? <Skeleton active paragraph={{ rows: 3 }} /> : assetsQuery.data?.assets.length ? <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">{assetsQuery.data.assets.map((item) => <div key={item.id} className="min-w-0 rounded-[var(--r-md)] border border-border bg-surface-subtle p-3"><div className="truncate text-sm font-medium">{item.asset.title || "未命名素材"}</div><div className="mt-1 truncate text-xs text-foreground/45">{(item.owner.displayName || item.owner.username) + " · " + kindLabel(item.asset.kind)}</div></div>)}</div> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有共享素材"><Button onClick={() => navigate("/assets")}>去素材库共享</Button></Empty>}</section>
                        <section className="min-w-0 rounded-[var(--r-lg)] border border-border bg-surface p-5"><SectionTitle icon={<Sparkles className="size-4" />} title="开始协作" detail="团队启用清单" /><ChecklistItem done={(membersQuery.data?.members.length || 0) > 1} title="邀请第一位成员" detail="通过限时链接加入，无需手工录入用户名" action={canManage ? openInvite : undefined} /><ChecklistItem done={(assetsQuery.data?.total || 0) > 0} title="共享第一份素材" detail="成员可统一浏览并复制到个人素材" action={() => navigate("/assets")} /><ChecklistItem done={Boolean(detailQuery.data?.team.description)} title="写下协作说明" detail="记录素材规范、项目目标或交付约定" action={activeTeam.role === "owner" ? () => setManageOpen(true) : undefined} /></section>
                    </div>
                    <div className="grid min-w-0 gap-4 lg:grid-cols-2">
                        <section className="min-w-0 rounded-[var(--r-lg)] border border-border bg-surface p-5"><SectionTitle icon={<UsersRound className="size-4" />} title="成员" detail={(membersQuery.data?.members.length || 0) + " 人"} action={<Button type="text" onClick={() => setManageOpen(true)}>管理</Button>} /><div className="space-y-2">{(membersQuery.data?.members || []).slice(0, 5).map((member) => <div key={member.userId} className="flex items-center gap-3 rounded-[var(--r-md)] px-2 py-2 hover:bg-surface-hover"><Avatar size="small" className="bg-[var(--workspace-accent)]">{(member.displayName || member.username).slice(0, 1)}</Avatar><div className="min-w-0 flex-1"><div className="truncate text-sm font-medium">{member.displayName || member.username}</div><div className="truncate text-xs text-foreground/40">@{member.username}</div></div><Tag className="m-0">{roleLabels[member.role]}</Tag></div>)}</div></section>
                        <section className="min-w-0 rounded-[var(--r-lg)] border border-border bg-surface p-5"><SectionTitle icon={<History className="size-4" />} title="最近活动" detail={canManage ? "管理者可见" : "仅管理者可见"} />{!canManage ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="你的角色无权查看审计记录" /> : auditQuery.data?.events.length ? <div className="space-y-3">{auditQuery.data.events.map((event) => <div key={event.id} className="flex gap-3"><span className="mt-1 size-2 shrink-0 rounded-full bg-[var(--workspace-accent)]" /><div className="min-w-0"><div className="text-sm"><span className="font-medium">{event.actorDisplayName || event.actorUsername}</span> <span className="text-foreground/60">{event.summary}</span></div><div className="mt-1 text-xs text-foreground/40">{formatDate(event.createdAt)}</div></div></div>)}</div> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无团队活动" />}</section>
                    </div>
                </div>
            </div>}
        </WorkspacePage>
        <Modal title="创建团队工作区" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={() => createMutation.mutate()} okText="创建" confirmLoading={createMutation.isPending} okButtonProps={{ disabled: !teamName.trim() }} destroyOnHidden><p className="mb-4 text-sm text-foreground/55">创建后你将成为所有者，可以邀请成员、设置配额并管理共享素材。</p><Input value={teamName} maxLength={120} showCount placeholder="例如：浮光短剧制作组" onChange={(event) => setTeamName(event.target.value)} onPressEnter={() => { if (teamName.trim()) createMutation.mutate(); }} /></Modal>
        <Modal title="邀请成员" open={inviteOpen} onCancel={() => setInviteOpen(false)} footer={null} destroyOnHidden>{createdInviteUrl ? <div className="space-y-4"><div className="rounded-[var(--r-lg)] border border-border bg-surface-subtle p-4"><div className="mb-2 flex items-center gap-2 text-sm font-medium"><Link2 className="size-4" />邀请链接已生成</div><Input.TextArea value={createdInviteUrl} readOnly autoSize={{ minRows: 2, maxRows: 4 }} /><Button className="mt-3" type="primary" icon={<Copy className="size-4" />} onClick={() => void copyText(createdInviteUrl)}>复制邀请链接</Button></div><p className="text-xs leading-5 text-foreground/45">完整令牌只在创建时显示。关闭后无法再次查看，但可以撤销并创建新链接。</p></div> : <Form layout="vertical" onFinish={() => inviteMutation.mutate()}><Form.Item label="加入后的角色"><Select value={inviteRole} onChange={setInviteRole} options={(activeTeam?.role === "owner" ? ["admin", "editor", "viewer"] : ["editor", "viewer"]).map((role) => ({ value: role, label: roleLabels[role as TeamRole] }))} /></Form.Item><Form.Item label="链接有效期"><Select value={inviteHours} onChange={setInviteHours} options={[{ value: 24, label: "24 小时" }, { value: 72, label: "3 天" }, { value: 168, label: "7 天" }, { value: 720, label: "30 天" }]} /></Form.Item><Button block type="primary" htmlType="submit" icon={<Link2 className="size-4" />} loading={inviteMutation.isPending}>生成一次性邀请链接</Button></Form>}{(invitationsQuery.data?.invitations.length || 0) > 0 ? <div className="mt-5 border-t border-border pt-4"><div className="mb-2 text-sm font-medium">待使用邀请</div><div className="space-y-2">{invitationsQuery.data?.invitations.map((invitation) => <InvitationRow key={invitation.id} invitation={invitation} revoking={revokeMutation.isPending && revokeMutation.variables === invitation.id} onRevoke={() => revokeMutation.mutate(invitation.id)} />)}</div></div> : null}</Modal>
        <TeamMemberManager open={manageOpen} team={activeTeam} onClose={() => setManageOpen(false)} onLeftTeam={(leftTeamId) => { setManageOpen(false); setActiveTeamId((current) => current === leftTeamId ? "" : current); void queryClient.invalidateQueries({ queryKey: ["teams"] }); }} />
    </>;
}

function UsageCards({ usage }: { usage: { memberCount: number; assetCount: number; assetLimit: number; storageBytes: number; storageLimitBytes: number } }) { return <div className="grid gap-3 sm:grid-cols-3"><Metric icon={<UsersRound className="size-4" />} label="活跃成员" value={String(usage.memberCount)} detail="正在协作" /><Metric icon={<Images className="size-4" />} label="共享素材" value={String(usage.assetCount)} detail={"上限 " + usage.assetLimit} progress={percentage(usage.assetCount, usage.assetLimit)} /><Metric icon={<Database className="size-4" />} label="共享存储" value={formatBytes(usage.storageBytes)} detail={"上限 " + formatBytes(usage.storageLimitBytes)} progress={percentage(usage.storageBytes, usage.storageLimitBytes)} /></div>; }
function Metric({ icon, label, value, detail, progress }: { icon: ReactNode; label: string; value: string; detail: string; progress?: number }) { return <div className="rounded-[var(--r-lg)] border border-border bg-surface p-4"><div className="flex items-center gap-2 text-xs text-foreground/45">{icon}{label}</div><div className="mt-3 text-2xl font-semibold tabular-nums">{value}</div><div className="mt-1 text-xs text-foreground/40">{detail}</div>{progress !== undefined ? <Progress className="mt-2" percent={progress} showInfo={false} size="small" status={progress >= 100 ? "exception" : "normal"} /> : null}</div>; }
function SectionTitle({ icon, title, detail, action }: { icon: ReactNode; title: string; detail?: string; action?: ReactNode }) { return <div className="mb-4 flex items-center justify-between gap-3"><div className="flex min-w-0 items-center gap-2"><span className="text-foreground/55">{icon}</span><h3 className="truncate text-sm font-semibold">{title}</h3>{detail ? <span className="text-xs text-foreground/40">{detail}</span> : null}</div>{action}</div>; }
function ChecklistItem({ done, title, detail, action }: { done: boolean; title: string; detail: string; action?: () => void }) { return <button type="button" disabled={!action} className="flex w-full items-start gap-3 rounded-[var(--r-md)] px-2 py-3 text-left transition-colors enabled:hover:bg-surface-hover" onClick={action}><span className={cn("mt-0.5 grid size-5 shrink-0 place-items-center rounded-full border text-[10px]", done ? "border-[var(--status-success)] text-[var(--status-success)]" : "border-border text-foreground/35")}>{done ? "✓" : ""}</span><span className="min-w-0 flex-1"><span className="block text-sm font-medium">{title}</span><span className="mt-1 block text-xs leading-5 text-foreground/45">{detail}</span></span>{action ? <ArrowRight className="mt-1 size-3.5 text-foreground/35" /> : null}</button>; }
function InvitationRow({ invitation, revoking, onRevoke }: { invitation: TeamInvitation; revoking: boolean; onRevoke: () => void }) { return <div className="flex items-center gap-3 rounded-[var(--r-md)] border border-border p-3"><CalendarClock className="size-4 shrink-0 text-foreground/45" /><div className="min-w-0 flex-1"><div className="text-sm">{roleLabels[invitation.role]}邀请</div><div className="mt-0.5 text-xs text-foreground/40">{formatDate(invitation.expiresAt)} 到期</div></div><Popconfirm title="撤销这个邀请？" okText="撤销" cancelText="取消" onConfirm={onRevoke}><Button danger type="text" size="small" loading={revoking}>撤销</Button></Popconfirm></div>; }
function FirstTeam({ onCreate }: { onCreate: () => void }) { return <section className="mx-auto mt-12 max-w-3xl overflow-hidden rounded-[var(--r-xl)] border border-border bg-surface p-8 text-center sm:p-14"><div className="mx-auto grid size-16 place-items-center rounded-2xl bg-[color-mix(in_srgb,var(--workspace-accent)_14%,transparent)]"><UsersRound className="size-8 text-[var(--workspace-accent)]" /></div><h2 className="mt-6 text-2xl font-semibold">从一个真正的团队工作区开始</h2><p className="mx-auto mt-3 max-w-xl text-sm leading-6 text-foreground/55">通过邀请链接添加成员，集中管理共享素材、角色权限、配额和协作记录。</p><Button className="mt-6" type="primary" size="large" icon={<Plus className="size-4" />} onClick={onCreate}>创建第一个团队</Button></section>; }
function percentage(value: number, limit: number) { return limit <= 0 ? 100 : Math.min(100, Math.round((value / limit) * 100)); }
function formatBytes(bytes: number) { if (!bytes) return "0 B"; const units = ["B", "KB", "MB", "GB", "TB"]; const index = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024))); const value = bytes / 1024 ** index; return (value >= 100 || index === 0 ? value.toFixed(0) : value.toFixed(1)) + " " + units[index]; }
function formatDate(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? "未知时间" : date.toLocaleString("zh-CN", { hour12: false }); }
function kindLabel(kind: string) { return ({ text: "文本", image: "图片", video: "视频", audio: "音频", model: "3D 模型" } as Record<string, string>)[kind] || kind; }
function readError(error: unknown, fallback: string) { return error instanceof Error && error.message ? error.message : fallback; }
