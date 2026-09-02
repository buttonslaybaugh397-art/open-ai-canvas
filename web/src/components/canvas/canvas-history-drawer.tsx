import { useMemo, useState } from "react";
import { Button, Drawer, Empty, Input, Popconfirm, Tag, Tooltip } from "antd";
import { Clock3, ExternalLink, History, Search, Trash2, X } from "lucide-react";
import { useNavigate } from "react-router";

import { cn } from "@/lib/utils";
import { useCanvasHistoryStore } from "@/stores/canvas/use-canvas-history-store";
import { useCanvasStore } from "@/stores/canvas/use-canvas-store";

type TimelineItem = {
    id: string;
    title: string;
    isDeleted: boolean;
    createdAt: string;
    updatedAt: string;
    deletedAt?: string;
    nodeCount: number;
    timelineTime: string;
};

export function CanvasHistoryDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
    const navigate = useNavigate();
    const activeProjects = useCanvasStore((state) => state.projects);
    const deletedProjects = useCanvasHistoryStore((state) => state.deletedProjects);
    const removeDeletedItem = useCanvasHistoryStore((state) => state.removeDeletedHistoryItem);
    const clearDeletedHistory = useCanvasHistoryStore((state) => state.clearDeletedHistory);
    const [keyword, setKeyword] = useState("");
    const [filter, setFilter] = useState<"all" | "active" | "deleted">("all");

    const timelineItems = useMemo<TimelineItem[]>(() => {
        const active = activeProjects.map((project) => ({
            id: project.id,
            title: project.title || "未命名画布",
            isDeleted: false,
            createdAt: project.createdAt || project.updatedAt,
            updatedAt: project.updatedAt,
            nodeCount: project.nodes?.length || 0,
            timelineTime: project.updatedAt || project.createdAt,
        }));
        const deleted = deletedProjects.map((project) => ({ ...project, isDeleted: true, timelineTime: project.deletedAt }));
        return [...active, ...deleted].sort((a, b) => Date.parse(b.timelineTime) - Date.parse(a.timelineTime));
    }, [activeProjects, deletedProjects]);

    const visibleItems = useMemo(() => {
        const query = keyword.trim().toLowerCase();
        return timelineItems.filter((item) => {
            if (filter === "active" && item.isDeleted) return false;
            if (filter === "deleted" && !item.isDeleted) return false;
            return !query || item.title.toLowerCase().includes(query);
        });
    }, [filter, keyword, timelineItems]);

    const openProject = (id: string) => {
        navigate(`/canvas/${id}`);
        onClose();
    };

    return (
        <Drawer
            title={
                <div className="flex items-center justify-between gap-2 pr-2">
                    <div className="flex items-center gap-2"><History className="size-4 text-[var(--workspace-accent)]" /><span>画布历史</span></div>
                    {deletedProjects.length ? (
                        <Popconfirm title="清空所有已删除画布的历史记录？" okText="清空" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={clearDeletedHistory}>
                            <Button type="text" size="small" danger icon={<Trash2 className="size-3.5" />}>清理删除记录</Button>
                        </Popconfirm>
                    ) : null}
                </div>
            }
            placement="right"
            width={460}
            open={open}
            onClose={onClose}
            className="workspace-drawer canvas-history-drawer"
            styles={{ body: { padding: "16px 20px" } }}
        >
            <div className="space-y-4">
                <Input allowClear prefix={<Search className="size-3.5 text-foreground/40" />} value={keyword} onChange={(event) => setKeyword(event.target.value)} placeholder="搜索画布名称" />
                <div className="flex items-center gap-2 text-xs">
                    {[{ key: "all", label: `全部 ${timelineItems.length}` }, { key: "active", label: `活跃 ${activeProjects.length}` }, { key: "deleted", label: `已删除 ${deletedProjects.length}` }].map((item) => (
                        <button key={item.key} type="button" aria-pressed={filter === item.key} className={cn("rounded-lg border border-border/60 px-3 py-1.5 text-foreground/60 transition-colors hover:bg-surface-active", filter === item.key && "border-[var(--workspace-accent)] bg-surface-active text-foreground")} onClick={() => setFilter(item.key as typeof filter)}>{item.label}</button>
                    ))}
                </div>
                {visibleItems.length ? (
                    <div className="relative space-y-3 border-l border-border/70 pl-4">
                        {visibleItems.map((item) => (
                            <article key={`${item.id}:${item.deletedAt || "active"}`} className={cn("relative rounded-xl border border-border/70 bg-surface p-3.5", item.isDeleted && "opacity-75")}>
                                <span className={cn("absolute -left-[21px] top-4 size-2.5 rounded-full ring-2 ring-background", item.isDeleted ? "bg-destructive" : "bg-emerald-500")} />
                                <div className="flex items-start justify-between gap-3">
                                    <div className="min-w-0 space-y-2">
                                        <div className="flex items-center gap-2">
                                            <button type="button" disabled={item.isDeleted} className={cn("truncate text-left text-sm font-semibold", item.isDeleted ? "line-through" : "hover:text-[var(--workspace-accent)]")} onClick={() => openProject(item.id)}>{item.title}</button>
                                            <Tag color={item.isDeleted ? "error" : "success"} className="m-0">{item.isDeleted ? "已删除" : "活跃中"}</Tag>
                                        </div>
                                        <div className="space-y-1 text-xs text-foreground/50">
                                            <div className="flex items-center gap-1.5"><Clock3 className="size-3.5" /><span>创建于 {formatTimelineDate(item.createdAt)}</span></div>
                                            <div>{item.isDeleted ? `删除于 ${formatTimelineDate(item.deletedAt || "")}` : `更新于 ${formatTimelineDate(item.updatedAt)}`} · {item.nodeCount} 个节点</div>
                                        </div>
                                    </div>
                                    {item.isDeleted ? (
                                        <Tooltip title="移除此条历史记录"><Button type="text" size="small" danger icon={<X className="size-4" />} onClick={() => removeDeletedItem(item.id)} /></Tooltip>
                                    ) : (
                                        <Button type="text" size="small" aria-label={`打开 ${item.title}`} icon={<ExternalLink className="size-4" />} onClick={() => openProject(item.id)} />
                                    )}
                                </div>
                            </article>
                        ))}
                    </div>
                ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无匹配的历史记录" />}
            </div>
        </Drawer>
    );
}

function formatTimelineDate(value: string) {
    const date = new Date(value);
    if (!Number.isFinite(date.getTime())) return "--";
    return new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "short" }).format(date);
}
