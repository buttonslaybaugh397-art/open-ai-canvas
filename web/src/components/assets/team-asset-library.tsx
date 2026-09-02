import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button, Checkbox, Dropdown, Input, Modal, Pagination, Tag } from "antd";
import { Copy, Download, FolderInput, MoreHorizontal, PencilLine, Search, Trash2 } from "lucide-react";
import { useDeferredValue, useEffect, useMemo, useState } from "react";

import { AssetFolderBar, ASSET_FOLDER_ALL, ASSET_FOLDER_ROOT, type AssetFolderView } from "@/components/assets/asset-folder-bar";
import { AssetMediaPreview } from "@/components/asset-media-preview";
import { WorkspaceState } from "@/components/layout/workspace-state";
import { useCopyText } from "@/hooks/use-copy-text";
import { canWriteTeamAssets, createTeamAssetFolder, deleteTeamAsset, deleteTeamAssetFolder, importTeamAssets, listTeamAssetFolders, listTeamAssets, moveTeamAsset, renameTeamAssetFolder, type TeamAssetFolder, type TeamAssetItem, type TeamRole } from "@/services/api/team-assets";
import { useAssetStore, type Asset, type AssetKind } from "@/stores/use-asset-store";
import { useUserStore } from "@/stores/use-user-store";
import { downloadMediaFile } from "@/services/resource-download";

export type TeamLibraryAsset = Exclude<Asset, { kind: "entity" }> & { folderId?: string };

const teamKindOptions = [
    { label: "全部", value: "all" },
    { label: "文本", value: "text" },
    { label: "图片", value: "image" },
    { label: "视频", value: "video" },
    { label: "音频", value: "audio" },
    { label: "3D 模型", value: "model" },
];

export function useTeamAssetQuery(teamId: string, input: { page: number; pageSize: number; query?: string; kind?: Exclude<AssetKind, "entity">; folderId?: string }, enabled = true) {
    const userId = useUserStore((state) => state.user?.id || "");
    return useQuery({
        queryKey: ["team-assets", userId, teamId, input],
        queryFn: ({ signal }) => listTeamAssets(teamId, { ...input, signal }),
        enabled: enabled && Boolean(userId && teamId),
        placeholderData: (previous) => previous,
        staleTime: 15_000,
    });
}

export function useTeamAssetFolderQuery(teamId: string, enabled = true) {
    const userId = useUserStore((state) => state.user?.id || "");
    return useQuery({ queryKey: ["team-asset-folders", userId, teamId], queryFn: ({ signal }) => listTeamAssetFolders(teamId, signal), enabled: enabled && Boolean(userId && teamId), staleTime: 15_000 });
}

export function TeamAssetBrowser({ teamId, role, onSelect, allowedKinds, pageSize = 20, manageable = false, showFolders = true }: { teamId: string; role?: TeamRole; onSelect?: (asset: TeamLibraryAsset) => void; allowedKinds?: AssetKind[]; pageSize?: number; manageable?: boolean; showFolders?: boolean }) {
    const { message } = App.useApp();
    const copyText = useCopyText();
    const userId = useUserStore((state) => state.user?.id || "");
    const queryClient = useQueryClient();
    const [keyword, setKeyword] = useState("");
    const [kindFilter, setKindFilter] = useState("all");
    const [folderFilter, setFolderFilter] = useState(ASSET_FOLDER_ALL);
    const [page, setPage] = useState(1);
    const deferredKeyword = useDeferredValue(keyword.trim());
    const canManage = manageable && canWriteTeamAssets(role);
    const folderId = folderFilter === ASSET_FOLDER_ALL ? undefined : folderFilter === ASSET_FOLDER_ROOT ? "" : folderFilter;
    const query = useTeamAssetQuery(teamId, {
        page,
        pageSize,
        query: deferredKeyword || undefined,
        kind: kindFilter === "all" ? undefined : kindFilter as Exclude<AssetKind, "entity">,
        folderId,
    });
    const folderQuery = useTeamAssetFolderQuery(teamId, showFolders);
    const [deletingAsset, setDeletingAsset] = useState<TeamAssetItem | null>(null);
    const [previewItem, setPreviewItem] = useState<(TeamAssetItem & { asset: TeamLibraryAsset }) | null>(null);
    const [editingFolder, setEditingFolder] = useState<TeamAssetFolder | null>(null);
    const [deletingFolder, setDeletingFolder] = useState<TeamAssetFolder | null>(null);
    const [folderModalOpen, setFolderModalOpen] = useState(false);
    const [draftName, setDraftName] = useState("");
    const [selectedIds, setSelectedIds] = useState<string[]>([]);
    const allowed = useMemo(() => new Set(allowedKinds || teamKindOptions.slice(1).map((item) => item.value as AssetKind)), [allowedKinds]);
    const items = useMemo(() => (query.data?.assets || []).filter((item): item is TeamAssetItem & { asset: TeamLibraryAsset } => item.asset.kind !== "entity" && allowed.has(item.asset.kind)), [allowed, query.data?.assets]);
    const folders = folderQuery.data?.folders || [];
    const folderViews = useMemo<AssetFolderView[]>(() => folders.map((folder) => ({ id: folder.id, name: folder.name, canEdit: canManage && folder.canEdit, count: folder.count || 0 })), [canManage, folders]);
    const total = query.data?.total || 0;
    const refresh = () => Promise.all([
        queryClient.invalidateQueries({ queryKey: ["team-assets", userId, teamId] }),
        queryClient.invalidateQueries({ queryKey: ["team-asset-folders", userId, teamId] }),
    ]);

    useEffect(() => setPage(1), [folderFilter, deferredKeyword, kindFilter, teamId]);
    useEffect(() => setPage((current) => Math.min(current, Math.max(1, Math.ceil(total / pageSize)))), [pageSize, total]);

    const deleteAssetMutation = useMutation({
        mutationFn: () => deletingAsset ? deleteTeamAsset(teamId, deletingAsset.id || deletingAsset.asset.id) : Promise.reject(new Error("未选择素材")),
        onSuccess: () => { void refresh(); setDeletingAsset(null); message.success("团队素材已取消共享"); },
        onError: (error) => message.error(readError(error, "取消共享失败")),
    });
    const moveMutation = useMutation({
        mutationFn: ({ id, folderId }: { id: string; folderId: string }) => moveTeamAsset(teamId, id, folderId),
        onSuccess: () => { void refresh(); message.success("素材已移动"); },
        onError: (error) => message.error(readError(error, "移动素材失败")),
    });
    const saveFolderMutation = useMutation({
        mutationFn: () => editingFolder ? renameTeamAssetFolder(teamId, editingFolder.id, draftName.trim()) : createTeamAssetFolder(teamId, draftName.trim()),
        onSuccess: () => { void refresh(); setFolderModalOpen(false); setEditingFolder(null); message.success(editingFolder ? "文件夹已重命名" : "文件夹已创建"); },
        onError: (error) => message.error(readError(error, "保存文件夹失败")),
    });
    const deleteFolderMutation = useMutation({
        mutationFn: () => deletingFolder ? deleteTeamAssetFolder(teamId, deletingFolder.id) : Promise.reject(new Error("未选择文件夹")),
        onSuccess: () => { if (folderFilter === deletingFolder?.id) setFolderFilter(ASSET_FOLDER_ROOT); void refresh(); setDeletingFolder(null); message.success("文件夹已删除，素材已移回根目录"); },
        onError: (error) => message.error(readError(error, "删除文件夹失败")),
    });
    const importMutation = useMutation({
        mutationFn: (assetIds: string[]) => importTeamAssets(teamId, assetIds),
        onSuccess: ({ imported }) => {
            const current = useAssetStore.getState().assets;
            const importedIds = new Set(imported.map((item) => item.asset.id));
            useAssetStore.getState().replaceAssets([...imported.map((item) => item.asset), ...current.filter((asset) => !importedIds.has(asset.id))]);
            setSelectedIds([]);
            message.success("已复制 " + imported.length + " 个素材到“我的素材”");
        },
        onError: (error) => message.error(readError(error, "复制到我的素材失败")),
    });

    const openCreateFolder = () => { setEditingFolder(null); setDraftName(""); setFolderModalOpen(true); };
    const openRenameFolder = (folder: AssetFolderView) => { const target = folders.find((item) => item.id === folder.id); if (!target) return; setEditingFolder(target); setDraftName(target.name); setFolderModalOpen(true); };
    const openDeleteFolder = (folder: AssetFolderView) => { const target = folders.find((item) => item.id === folder.id); if (target) setDeletingFolder(target); };
    const importOne = (item: TeamAssetItem) => importMutation.mutate([item.id || item.asset.id]);

    useEffect(() => setSelectedIds([]), [teamId]);

    return (
        <div>
            <div className="flex flex-wrap items-center gap-3">
                <Input className="w-64" prefix={<Search className="size-3.5 text-foreground/40" />} placeholder="搜索团队素材或作者" value={keyword} allowClear onChange={(event) => setKeyword(event.target.value)} />
                <div className="flex flex-wrap gap-1.5">
                    {teamKindOptions.filter((option) => option.value === "all" || allowed.has(option.value as AssetKind)).map((option) => <Tag.CheckableTag key={option.value} checked={kindFilter === option.value} className={kindFilter === option.value ? "prompt-filter-tag is-active" : "prompt-filter-tag"} onChange={() => setKindFilter(option.value)}>{option.label}</Tag.CheckableTag>)}
                </div>
                {!canWriteTeamAssets(role) ? <Tag className="m-0">只读成员</Tag> : null}
            </div>
            {showFolders ? <AssetFolderBar folders={folderViews} value={folderFilter} rootCount={folderFilter === ASSET_FOLDER_ROOT ? total : 0} totalCount={folderFilter === ASSET_FOLDER_ALL ? total : 0} onChange={setFolderFilter} onCreate={canManage ? openCreateFolder : undefined} onRename={canManage ? openRenameFolder : undefined} onDelete={canManage ? openDeleteFolder : undefined} /> : null}
            {manageable && selectedIds.length ? <div className="mt-3 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-foreground/[.025] px-3 py-2"><span className="text-xs text-foreground/60">已选择 {selectedIds.length} 个团队素材</span><div className="flex gap-2"><Button size="small" onClick={() => setSelectedIds([])}>取消选择</Button><Button size="small" type="primary" icon={<Copy className="size-3.5" />} loading={importMutation.isPending} onClick={() => importMutation.mutate(selectedIds)}>复制到我的素材</Button></div></div> : null}
            {query.isLoading || (showFolders && folderQuery.isLoading) ? <WorkspaceState icon="assets" compact title="正在加载团队素材" description="团队共享素材从登录后端实时读取。" /> : query.isError || folderQuery.isError ? <WorkspaceState icon="assets" compact title="团队素材加载失败" description={readError(query.error || folderQuery.error, "请稍后重试。")} action={<Button onClick={() => { void query.refetch(); void folderQuery.refetch(); }}>重新加载</Button>} /> : items.length ? (
                <div className="mt-4 grid grid-cols-1 gap-x-4 gap-y-5 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
                    {items.map((item) => <TeamAssetCard key={item.id} item={item} selected={selectedIds.includes(item.id)} selectable={manageable} onSelect={(checked) => setSelectedIds((current) => checked ? [...new Set([...current, item.id])] : current.filter((id) => id !== item.id))} onClick={() => onSelect ? onSelect(item.asset) : setPreviewItem(item)} menuItems={teamAssetMenuItems(item, folders, setDeletingAsset, moveMutation.mutate, copyText, importOne, canManage)} />)}
                </div>
            ) : <WorkspaceState icon="assets" compact title="没有团队素材" description={keyword || kindFilter !== "all" || folderFilter !== ASSET_FOLDER_ALL ? "调整关键词、类型或文件夹后再试。" : "在“我的素材”中选择素材，然后共享到当前团队。"} />}
            {total > pageSize ? <div className="mt-4 flex justify-center"><Pagination size="small" current={page} pageSize={pageSize} total={total} showSizeChanger={false} onChange={setPage} /></div> : null}

            <Modal title="团队素材详情" open={Boolean(previewItem)} onCancel={() => setPreviewItem(null)} footer={null} width={760}>
                {previewItem ? <TeamAssetPreview item={previewItem} copyText={copyText} /> : null}
            </Modal>
            <Modal title={editingFolder ? "重命名团队文件夹" : "新建团队文件夹"} open={folderModalOpen} onCancel={() => setFolderModalOpen(false)} onOk={() => saveFolderMutation.mutate()} okText="保存" confirmLoading={saveFolderMutation.isPending} okButtonProps={{ disabled: !draftName.trim() }}><Input value={draftName} maxLength={120} placeholder="输入文件夹名称" onChange={(event) => setDraftName(event.target.value)} /></Modal>
            <Modal title="取消共享" open={Boolean(deletingAsset)} onCancel={() => setDeletingAsset(null)} onOk={() => deleteAssetMutation.mutate()} okText="取消共享" confirmLoading={deleteAssetMutation.isPending} okButtonProps={{ danger: true }}>确定从团队素材库移除「{deletingAsset?.asset.title}」吗？这不会删除上传者的个人素材和资源文件。</Modal>
            <Modal title="删除团队文件夹" open={Boolean(deletingFolder)} onCancel={() => setDeletingFolder(null)} onOk={() => deleteFolderMutation.mutate()} okText="删除文件夹" confirmLoading={deleteFolderMutation.isPending} okButtonProps={{ danger: true }}>确定删除「{deletingFolder?.name}」吗？文件夹内素材会移回根目录。</Modal>
        </div>
    );
}

export function TeamAssetLibraryModal({ open, onClose, teamId, role }: { open: boolean; onClose: () => void; teamId: string; role?: TeamRole }) {
    return <Modal title="团队共享素材库" open={open} onCancel={onClose} footer={null} width={1100} destroyOnHidden><TeamAssetBrowser teamId={teamId} role={role} manageable /></Modal>;
}

function TeamAssetCard({ item, selected, selectable, onSelect, onClick, menuItems }: { item: TeamAssetItem & { asset: TeamLibraryAsset }; selected?: boolean; selectable?: boolean; onSelect?: (checked: boolean) => void; onClick?: () => void; menuItems?: NonNullable<Parameters<typeof Dropdown>[0]["menu"]>["items"] }) {
    const asset = item.asset;
    const card = (
        <article className={"library-card library-card-surface asset-library-card group relative overflow-hidden " + (selected ? "ring-2 ring-[var(--workspace-accent)]" : "")}>
            <button type="button" className="block w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--workspace-accent)]" onClick={onClick}>
                <AssetMediaPreview asset={asset} alt={asset.title} className="aspect-[4/3] w-full bg-black object-cover" fallback={<div className="flex aspect-[4/3] items-center justify-center bg-foreground/[.04] p-4 text-center text-xs leading-5 text-foreground/55">{asset.kind === "text" ? asset.data.content : "暂无封面"}</div>} />
                <div className="p-2.5"><div className="flex min-w-0 items-center justify-between gap-2"><span className="truncate text-xs font-medium">{asset.title}</span><Tag className="m-0 shrink-0 text-[var(--fs-tiny)]">{teamAssetKindLabel(asset.kind)}</Tag></div><div className="mt-1 truncate text-[var(--fs-tiny)] text-foreground/45">{item.owner.displayName || item.owner.username}</div></div>
            </button>
            {selectable ? <Checkbox className="absolute left-2 top-2 rounded bg-background/85 p-1 backdrop-blur" checked={selected} aria-label={"选择团队素材：" + asset.title} onClick={(event) => event.stopPropagation()} onChange={(event) => onSelect?.(event.target.checked)} /> : null}
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

function teamAssetMenuItems(item: TeamAssetItem & { asset: TeamLibraryAsset }, folders: TeamAssetFolder[], setDeleting: (item: TeamAssetItem) => void, move: (input: { id: string; folderId: string }) => void, copyText: (value: string, successText?: string) => void, importOne: (item: TeamAssetItem) => void, canManage: boolean) {
    return [
        { key: "import", label: "复制到我的素材", icon: <Copy className="size-3.5" />, onClick: () => importOne(item) },
        ...(item.asset.kind === "text" ? [{ key: "copy", label: "复制文本", icon: <Copy className="size-3.5" />, onClick: () => copyTeamAssetText(item.asset, copyText) }] : []),
        ...(item.asset.kind !== "text" ? [{ key: "download", label: "下载", icon: <Download className="size-3.5" />, onClick: () => downloadTeamAsset(item.asset) }] : []),
        ...(canManage && item.canEdit ? [
            { type: "divider" as const },
            { key: "move", label: "移动到", icon: <FolderInput className="size-3.5" />, children: [
                { key: "move-root", label: "根目录", onClick: () => move({ id: item.id || item.asset.id, folderId: "" }) },
                ...folders.map((folder) => ({ key: "move-" + folder.id, label: folder.name, onClick: () => move({ id: item.id || item.asset.id, folderId: folder.id }) })),
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
    void downloadMediaFile({ url, storageKey: asset.data.storageKey, fileName: `${asset.title || "asset"}.${extension}` }).catch(() => undefined);
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
