import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Result, Skeleton, Tag } from "antd";
import { ArrowRight, Clock3, ShieldCheck, UsersRound } from "lucide-react";
import { useNavigate, useParams } from "react-router";

import { WorkspacePage } from "@/components/layout/workspace-page";
import { acceptTeamInvitation, getTeamInvitation, type TeamRole } from "@/services/api/team-assets";

const roleLabels: Record<TeamRole, string> = { owner: "所有者", admin: "管理员", editor: "编辑者", viewer: "查看者" };

export default function TeamJoinPage() {
    const { message } = App.useApp();
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { token = "" } = useParams();
    const previewQuery = useQuery({ queryKey: ["team-invitation-preview", token], queryFn: ({ signal }) => getTeamInvitation(token, signal), enabled: Boolean(token), retry: false });
    const acceptMutation = useMutation({
        mutationFn: () => acceptTeamInvitation(token),
        onSuccess: async ({ team }) => {
            await queryClient.invalidateQueries({ queryKey: ["teams"] });
            message.success("已加入 " + team.name);
            navigate("/teams", { replace: true });
        },
        onError: (error) => message.error(error instanceof Error ? error.message : "加入团队失败"),
    });

    return <WorkspacePage grid className="teams-workspace-page"><div className="mx-auto flex min-h-[70vh] max-w-2xl items-center justify-center py-10"><section className="w-full overflow-hidden rounded-[var(--r-xl)] border border-border bg-surface p-6 text-center shadow-sm sm:p-10">
        {previewQuery.isLoading ? <Skeleton active paragraph={{ rows: 4 }} /> : previewQuery.isError ? <Result status="error" title="邀请链接无效" subTitle={previewQuery.error instanceof Error ? previewQuery.error.message : "链接可能已过期、被撤销或不存在"} extra={<Button onClick={() => navigate("/teams")}>返回团队工作区</Button>} /> : previewQuery.data ? <>
            <div className="mx-auto grid size-16 place-items-center rounded-2xl bg-[color-mix(in_srgb,var(--workspace-accent)_14%,transparent)]"><UsersRound className="size-8 text-[var(--workspace-accent)]" /></div>
            <div className="mt-5 text-xs font-medium tracking-[.18em] text-foreground/40">团队邀请</div>
            <h1 className="mt-2 text-2xl font-semibold sm:text-3xl">加入 {previewQuery.data.teamName}</h1>
            <p className="mx-auto mt-3 max-w-lg text-sm leading-6 text-foreground/55">接受后你将立刻获得团队共享素材和协作空间的访问权限。</p>
            <div className="mx-auto mt-6 grid max-w-md gap-3 text-left sm:grid-cols-2"><div className="rounded-[var(--r-lg)] border border-border bg-surface-subtle p-4"><div className="flex items-center gap-2 text-xs text-foreground/45"><ShieldCheck className="size-4" />加入角色</div><Tag className="mt-3">{roleLabels[previewQuery.data.role]}</Tag></div><div className="rounded-[var(--r-lg)] border border-border bg-surface-subtle p-4"><div className="flex items-center gap-2 text-xs text-foreground/45"><Clock3 className="size-4" />有效期至</div><div className="mt-3 text-sm font-medium">{new Date(previewQuery.data.expiresAt).toLocaleString("zh-CN", { hour12: false })}</div></div></div>
            {previewQuery.data.available ? <Button className="mt-7" type="primary" size="large" loading={acceptMutation.isPending} onClick={() => acceptMutation.mutate()}>接受邀请并加入 <ArrowRight className="ml-1 inline size-4" /></Button> : <Result status="warning" title="这个邀请已失效" subTitle="请联系团队管理员创建新的邀请链接。" extra={<Button onClick={() => navigate("/teams")}>返回团队工作区</Button>} />}
        </> : null}
    </section></div></WorkspacePage>;
}
