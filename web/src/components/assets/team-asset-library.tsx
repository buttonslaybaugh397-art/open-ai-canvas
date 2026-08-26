import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Dropdown, Input, Modal, Pagination, Tag } from "antd";
import { Copy, Download, FolderInput, MoreHorizontal, PencilLine, Search, Trash2 } from "lucide-react";
import { saveAs } from "file-saver";
import { useEffect, useMemo, useState } from "react";

import { AssetFolderBar, ASSET_FOLDER_ALL, ASSET_FOLDER_ROOT, type AssetFolderView } from "@/components/assets/asset-folder-bar";
import { AssetMediaPreview } from "@/components/asset-media-preview";
import { WorkspaceState } from "@/components/layout/workspace-state";
import { useCopyText } from "@/hooks/use-copy-text";
import { createTeamAssetFolder, deleteTeamAsset, deleteTeamAssetFolder, listTeamAssetFolders, listTeamAssets, moveTeamAsset, renameTeamAssetFolder, upsertTeamAsset, type TeamAssetFolder, type TeamAssetItem } from "@/services/api/team-assets";
import type { Asset, AssetKind } from "@/stores/use-asset-store";
import { useUserStore } from "@/stores/use-user-store";

export type TeamLibraryAsset = Exclude<Asset, { kind: "entity" }> & { folderId?: string };

const teamKindOptions = [
    { label: "全部", value: "all" },
    { label: "文本", value: "text" },
    { label: "图片", value: "image" },
    { label: "视频", value: "video" },
    { label: "音频", value: "audio" },
    { label: "3D 模型", value: "model" },
];

export function useTeamAssetQuery(enabled = true) {
    const userId = useUserStore((state) => state.user?.id || "");
    return useQuery({ queryKey: ["team-assets", userId], queryFn: listTeamAssets, enabled: enabled && Boolean(userId), staleTime: 15_000 });
}

export function useTeamAssetFolderQuery(enabled = true) {
    const userId = useUserStore((state) => state.user?.id || "");
    return useQuery({ queryKey: ["team-asset-folders", userId], queryFn: listTeamAssetFolders, enabled: enabled && Boolean(userId), staleTime: 15_000 });
}

export function TeamAssetBrowser({ onSelect, allowedKinds, pageSize = 20, manageable = false, showFolders = true }: { onSelect?: (asset: TeamLibraryAsset) => void; allowedKinds?: AssetKind[]; pageSize?: number; manageable?: boolean; showFolders?: boolean }) {
    const { message } = App.useApp();
    const copyText = useCopyText();
    const userId = useUserStore((state) => state.user?.id || "");
    const queryClient = useQueryClient();
    const query = useTeamAssetQuery();
    const folderQuery = useTeamAssetFolderQuery(showFolders);
    const [keyword, setKeyword] = useState("");
    const [kindFilter, setKindFilter] = useState("all");
    const [folderFilter, setFolderFilter] = useState(ASSET_FOLDER_ALL);
    const [page, setPage] = useState(1);
    const [editingAsset, setEditingAsset] = useState<TeamAssetItem | null>(null);
    const [deletingAsset, setDeletingAsset] = useState<TeamAssetItem | null>(null);
    const [previewItem, setPreviewItem] = useState<(TeamAssetItem & { asset: TeamLibraryAsset }) | null>(null);
    const [editingFolder, setEditingFolder] = useState<TeamAssetFolder | null>(null);
    const [deletingFolder, setDeletingFolder] = useState<TeamAssetFolder | null>(null);
    const [folderModalOpen, setFolderModalOpen] = useState(false);
    const [draftName, setDraftName] = useState("");
    const allowed = useMemo(() => new Set(allowedKinds || teamKindOptions.slice(1).map((item) => item.value as AssetKind)), [allowedKinds]);
    const items = useMemo(() => (query.data?.assets || []).filter((item): item is TeamAssetItem & { asset: TeamLibraryAsset } => item.asset.kind !== "entity" && allowed.has(item.asset.kind)), [allowed, query.data?.assets]);
    const folders = folderQuery.data?.folders || [];
    const folderViews = useMemo<AssetFolderView[]>(() => folders.map((folder) => ({ id: folder.id, name: folder.name, canEdit: folder.canEdit, ownerLabel: folder.owner.displayName || folder.owner.username, count: items.filter((item) => item.asset.folderId === folder.id).length })), [folders, items]);
    const filtered = useMemo(() => {
        const search = keyword.trim().toLowerCase();
        return items
            .filter((item) => kindFilter === "all" || item.asset.kind === kindFilter)
            .filter((item) => folderFilter === ASSET_FOLDER_ALL || (folderFilter === ASSET_FOLDER_ROOT ? !item.asset.folderId : item.asset.folderId === folderFilter))
            .filter((item) => !search || [item.asset.title, item.owner.displayName, item.owner.username, ...(item.asset.tags || [])].join(" ").toLowerCase().includes(search));
    }, [folderFilter, items, keyword, kindFilter]);
    const visible = useMemo(() => filtered.slice((page - 1) * pageSize, page * pageSize), [filtered, page, pageSize]);
    const refresh = () => Promise.all([
        queryClient.invalidateQueries({ queryKey: ["team-assets", userId] }),
        queryClient.invalidateQueries({ queryKey: ["team-asset-folders", userId] }),
    ]);

    useEffect(() => setPage(1), [folderFilter, keyword, kindFilter]);
    useEffect(() => setPage((current) => Math.min(current, Math.max(1, Math.ceil(filtered.length / pageSize)))), [filtered.length, pageSize]);

    const renameAssetMutation = useMutation({
        mutationFn: () => editingAsset ? upsertTeamAsset({ ...editingAsset.asset, title: draftName.trim(), updatedAt: new Date().toISOString() }) : Promise.reject(new Error("未选择素材")),
        onSuccess: () => { void refresh(); setEditingAsset(null); message.success("团队素材已更新"); },
        onError: (error) => message.error(readError(error, "团队素材更新失败")),
    });
    const deleteAssetMutation = useMutation({
        mutationFn: () => deletingAsset ? deleteTeamAsset(deletingAsset.asset.id) : Promise.reject(new Error("未选择素材")),
        onSuccess: () => { void refresh(); setDeletingAsset(null); message.success("团队素材已取消共享"); },
        onError: (error) => message.error(readError(error, "取消共享失败")),
    });
    const moveMutation = useMutation({
        mutationFn: ({ id, folderId }: { id: string; folderId: string }) => moveTeamAsset(id, folderId),
        onSuccess: () => { void refresh(); message.success("素材已移动"); },
        onError: (error) => message.error(readError(error, "移动素材失败")),
    });
    const saveFolderMutation = useMutation({
        mutationFn: () => editingFolder ? renameTeamAssetFolder(editingFolder.id, draftName.trim()) : createTeamAssetFolder(draftName.trim()),
        onSuccess: () => { void refresh(); setFolderModalOpen(false); setEditingFolder(null); message.success(editingFolder ? "文件夹已重命名" : "文件夹已创建"); },
        onError: (error) => message.error(readError(error, "保存文件夹失败")),
    });
    const deleteFolderMutation = useMutation({
        mutationFn: () => deletingFolder ? deleteTeamAssetFolder(deletingFolder.id) : Promise.reject(new Error("未选择文件夹")),
        onSuccess: () => { if (folderFilter === deletingFolder?.id) setFolderFilter(ASSET_FOLDER_ROOT); void refresh(); setDeletingFolder(null); message.success("文件夹已删除，素材已移回根目录"); },
        onError: (error) => message.error(readError(error, "删除文件夹失败")),
    });

    const openCreateFolder = () => { setEditingFolder(null); setDraftName(""); setFolderModalOpen(true); };
    const openRenameFolder = (folder: AssetFolderView) => { const target = folders.find((item) => item.id === folder.id); if (!target) return; setEditingFolder(target); setDraftName(target.name); setFolderModalOpen(true); };
    const openDeleteFolder = (folder: AssetFolderView) => { const target = folders.find((item) => item.id === folder.id); if (target) setDeletingFolder(target); };

    return (
        <div>
            <div className="flex flex-wrap items-center gap-3">
                <Input className="w-64" prefix={<Search className="size-3.5 text-foreground/40" />} placeholder="搜索团队素材或作者" value={keyword} allowClear onChange={(event) => setKeyword(event.target.value)} />
                <div className="flex flex-wrap gap-1.5">
                    {teamKindOptions.filter((option) => option.value === "all" || allowed.has(option.value as AssetKind)).map((option) => <Tag.CheckableTag key={option.value} checked={kindFilter === option.value} className={kindFilter === option.value ? "prompt-filter-tag is-active" : "prompt-filter-tag"} onChange={() => setKindFilter(option.value)}>{option.label}</Tag.CheckableTag>)}
                </div>
            </div>
            {showFolders ? <AssetFolderBar folders={folderViews} value={folderFilter} rootCount={items.filter((item) => !item.asset.folderId).length} totalCount={items.length} onChange={setFolderFilter} onCreate={manageable ? openCreateFolder : undefined} onRename={manageable ? openRenameFolder : undefined} onDelete={manageable ? openDeleteFolder : undefined} /> : null}
            {query.isLoading || (showFolders && folderQuery.isLoading) ? <WorkspaceState icon="assets" compact title="正在加载团队素材" description="团队共享素材从登录后端实时读取。" /> : query.isError || folderQuery.isError ? <WorkspaceState icon="assets" compact title="团队素材加载失败" description={readError(query.error || folderQuery.error, "请稍后重试。")} action={<Button onClick={() => { void query.refetch(); void folderQuery.refetch(); }}>重新加载</Button>} /> : visible.length ? (
                <div className="mt-4 grid grid-cols-1 gap-x-4 gap-y-5 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
                    {visible.map((item) => <TeamAssetCard key={item.asset.id} item={item} onClick={() => onSelect ? onSelect(item.asset) : setPreviewItem(item)} menuItems={manageable ? teamAssetMenuItems(item, folders, setEditingAsset, setDeletingAsset, setDraftName, moveMutation.mutate, copyText) : undefined} />)}
                </div>
            ) : <WorkspaceState icon="assets" compact title="没有团队素材" description={keyword || kindFilter !== "all" || folderFilter !== ASSET_FOLDER_ALL ? "调整关键词、类型或文件夹后再试。" : "从个人素材卡片的菜单中选择“共享到团队”。"} />}
            {filtered.length > pageSize ? <div className="mt-4 flex justify-center"><Pagination size="small" current={page} pageSize={pageSize} total={filtered.length} showSizeChanger={false} onChange={setPage} /></div> : null}

            <Modal title="团队素材详情" open={Boolean(previewItem)} onCancel={() => setPreviewItem(null)} footer={null} width={760}>
                {previewItem ? <TeamAssetPreview item={previewItem} copyText={copyText} /> : null}
            </Modal>
            <Modal title="重命名团队素材" open={Boolean(editingAsset)} onCancel={() => setEditingAsset(null)} onOk={() => renameAssetMutation.mutate()} okText="保存" confirmLoading={renameAssetMutation.isPending} okButtonProps={{ disabled: !draftName.trim() }}><Input value={draftName} maxLength={120} onChange={(event) => setDraftName(event.target.value)} /></Modal>
            <Modal title={editingFolder ? "重命名团队文件夹" : "新建团队文件夹"} open={folderModalOpen} onCancel={() => setFolderModalOpen(false)} onOk={() => saveFolderMutation.mutate()} okText="保存" confirmLoading={saveFolderMutation.isPending} okButtonProps={{ disabled: !draftName.trim() }}><Input value={draftName} maxLength={120} placeholder="输入文件夹名称" onChange={(event) => setDraftName(event.target.value)} /></Modal>
            <Modal title="取消共享" open={Boolean(deletingAsset)} onCancel={() => setDeletingAsset(null)} onOk={() => deleteAssetMutation.mutate()} okText="取消共享" confirmLoading={deleteAssetMutation.isPending} okButtonProps={{ danger: true }}>确定从团队素材库移除「{deletingAsset?.asset.title}」吗？这不会删除上传者的个人素材和资源文件。</Modal>
            <Modal title="删除团队文件夹" open={Boolean(deletingFolder)} onCancel={() => setDeletingFolder(null)} onOk={() => deleteFolderMutation.mutate()} okText="删除文件夹" confirmLoading={deleteFolderMutation.isPending} okButtonProps={{ danger: true }}>确定删除「{deletingFolder?.name}」吗？文件夹内素材会移回根目录。</Modal>
        </div>
    );
}

export function TeamAssetLibraryModal({ open, onClose }: { open: boolean; onClose: () => void }) {
    return <Modal title="团队共享素材库" open={open} onCancel={onClose} footer={null} width={1100} destroyOnHidden><TeamAssetBrowser manageable /></Modal>;
}

function TeamAssetCard({ item, onClick, menuItems }: { item: TeamAssetItem & { asset: TeamLibraryAsset }; onClick?: () => void; menuItems?: NonNullable<Parameters<typeof Dropdown>[0]["menu"]>["items"] }) {
    const asset = item.asset;
    const card = (
        <article className="library-card library-card-surface asset-library-card group relative overflow-hidden">
            <button type="button" className="block w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--workspace-accent)]" onClick={onClick}>
                <AssetMediaPreview asset={asset} alt={asset.title} className="aspect-[4/3] w-full bg-black object-cover" fallback={<div className="flex aspect-[4/3] items-center justify-center bg-foreground/[.04] p-4 text-center text-xs leading-5 text-foreground/55">{asset.kind === "text" ? asset.data.content : "暂无封面"}</div>} />
                <div className="p-2.5"><div className="flex min-w-0 items-center justify-between gap-2"><span className="truncate text-xs font-medium">{asset.title}</span><Tag className="m-0 shrink-0 text-[var(--fs-tiny)]">{teamAssetKindLabel(asset.kind)}</Tag></div><div className="mt-1 truncate text-[var(--fs-tiny)] text-foreground/45">{item.owner.displayName || item.owner.username}</div></div>
            </button>
            {menuItems?.length ? <Dropdown trigger={["click"]} menu={{ items: menuItems }}><button type="button" aria-label={`管理团队素材：${asset.title}`} className="absolute right-2 top-2 grid size-8 place-items-center rounded-md bg-black/55 text-white opacity-100 backdrop-blur sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100"><MoreHorizontal className="size-4" /></button></Dropdown> : null}
        </article>
    );
    return menuItems?.length ? <Dropdown trigger={["contextMenu"]} menu={{ items: menuItems }}><div>{card}</div></Dropdown> : card;
}

function TeamAssetPreview({ item, copyText }: { item: TeamAssetItem & { asset: TeamLibraryAsset }; copyText: (value: string, successText?: string) => void }) {
    const asset = item.asset;
    return <div>
        <AssetMediaPreview asset={asset} alt={asset.title} className="max-h-96 w-full rounded-md bg-black object-contain" fallback={<div className="rounded-md border border-border bg-foreground/5 p-5 text-sm leading-6 text-foreground/65">{asset.kind === "text" ? asset.data.content : "暂无封面"}</div>} />
        <div className="mt-3 flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0"><div className="text-sm font-semibold">{asset.title}</div><div className="mt-1 text-xs text-foreground/45">分享者：{item.owner.displayName || item.owner.username}</div></div>
            {asset.kind === "text" ? <Button icon={<Copy className="size-3.5" />} onClick={() => copyTeamAssetText(asset, copyText)}>复制文本</Button> : <Button icon={<Download className="size-3.5" />} onClick={() => downloadTeamAsset(asset)}>下载</Button>}
        </div>
    </div>;
}

function teamAssetMenuItems(item: TeamAssetItem & { asset: TeamLibraryAsset }, folders: TeamAssetFolder[], setEditing: (item: TeamAssetItem) => void, setDeleting: (item: TeamAssetItem) => void, setDraftName: (name: string) => void, move: (input: { id: string; folderId: string }) => void, copyText: (value: string, successText?: string) => void) {
    return [
        ...(item.asset.kind === "text" ? [{ key: "copy", label: "复制文本", icon: <Copy className="size-3.5" />, onClick: () => copyTeamAssetText(item.asset, copyText) }] : []),
        ...(item.asset.kind !== "text" ? [{ key: "download", label: "下载", icon: <Download className="size-3.5" />, onClick: () => downloadTeamAsset(item.asset) }] : []),
        ...(item.canEdit ? [
            { type: "divider" as const },
            { key: "rename", label: "重命名", icon: <PencilLine className="size-3.5" />, onClick: () => { setEditing(item); setDraftName(item.asset.title); } },
            { key: "move", label: "移动到", icon: <FolderInput className="size-3.5" />, children: [
                { key: "move-root", label: "根目录", onClick: () => move({ id: item.asset.id, folderId: "" }) },
                ...folders.map((folder) => ({ key: "move-" + folder.id, label: folder.name, onClick: () => move({ id: item.asset.id, folderId: folder.id }) })),
            ] },
            { type: "divider" as const },
            { key: "delete", label: "取消共享", danger: true, icon: <Trash2 className="size-3.5" />, onClick: () => setDeleting(item) },
        ] : []),
    ];
}

function downloadTeamAsset(asset: TeamLibraryAsset) {
    if (asset.kind === "text") return;
    const url = asset.kind === "image" ? asset.data.dataUrl : asset.data.url;
    const extension = asset.kind === "model" ? asset.data.fileName.split(".").pop() || "glb" : asset.data.mimeType.split("/")[1]?.split(";")[0] || "bin";
    saveAs(url, (asset.title || "asset") + "." + extension);
}

function copyTeamAssetText(asset: TeamLibraryAsset, copyText: (value: string, successText?: string) => void) {
    if (asset.kind === "text") copyText(asset.data.content, "文本已复制");
}

function teamAssetKindLabel(kind: AssetKind) {
    return kind === "image" ? "图片" : kind === "video" ? "视频" : kind === "audio" ? "音频" : kind === "model" ? "3D" : "文本";
}

function readError(error: unknown, fallback: string) {
    return error instanceof Error ? error.message : fallback;
}
