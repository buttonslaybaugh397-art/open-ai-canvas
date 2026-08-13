import { Button, Dropdown } from "antd";
import { Folder, FolderOpen, FolderPlus, MoreHorizontal, PencilLine, Trash2 } from "lucide-react";

export const ASSET_FOLDER_ALL = "__all__";
export const ASSET_FOLDER_ROOT = "__root__";

export type AssetFolderView = {
    id: string;
    name: string;
    count: number;
    canEdit?: boolean;
    ownerLabel?: string;
};

export function AssetFolderBar({ folders, value, rootCount, totalCount, onChange, onCreate, onRename, onDelete }: { folders: AssetFolderView[]; value: string; rootCount: number; totalCount: number; onChange: (id: string) => void; onCreate?: () => void; onRename?: (folder: AssetFolderView) => void; onDelete?: (folder: AssetFolderView) => void }) {
    return (
        <section className="mt-3 border-b border-border/75 pb-3" aria-label="素材文件夹">
            <div className="mb-2 flex items-center justify-between gap-3">
                <div className="text-xs font-semibold text-foreground/65">文件夹</div>
                {onCreate ? <Button size="small" type="text" icon={<FolderPlus className="size-3.5" />} onClick={onCreate}>新建文件夹</Button> : null}
            </div>
            <div className="flex gap-2 overflow-x-auto pb-1">
                <FolderButton id={ASSET_FOLDER_ALL} name="全部素材" count={totalCount} active={value === ASSET_FOLDER_ALL} onClick={onChange} />
                <FolderButton id={ASSET_FOLDER_ROOT} name="根目录" count={rootCount} active={value === ASSET_FOLDER_ROOT} onClick={onChange} />
                {folders.map((folder) => <FolderButton key={folder.id} {...folder} active={value === folder.id} onClick={onChange} onRename={onRename} onDelete={onDelete} />)}
            </div>
        </section>
    );
}

function FolderButton({ id, name, count, active, canEdit, ownerLabel, onClick, onRename, onDelete }: AssetFolderView & { active: boolean; onClick: (id: string) => void; onRename?: (folder: AssetFolderView) => void; onDelete?: (folder: AssetFolderView) => void }) {
    const folder = { id, name, count, canEdit, ownerLabel };
    const menu = canEdit && onRename && onDelete ? { items: [
        { key: "rename", label: "重命名", icon: <PencilLine className="size-3.5" />, onClick: () => onRename(folder) },
        { key: "delete", label: "删除文件夹", danger: true, icon: <Trash2 className="size-3.5" />, onClick: () => onDelete(folder) },
    ] } : undefined;
    const content = (
        <div className={`flex h-12 min-w-40 items-center gap-2 rounded-md border px-2.5 text-left transition-colors ${active ? "border-[var(--workspace-accent)] bg-[var(--workspace-accent-soft)]" : "border-border bg-background hover:border-foreground/25 hover:bg-foreground/5"}`}>
            {active ? <FolderOpen className="size-4 shrink-0 text-[var(--workspace-accent)]" /> : <Folder className="size-4 shrink-0 text-foreground/48" />}
            <button type="button" className="min-w-0 flex-1 text-left focus-visible:outline-none" onClick={() => onClick(id)}>
                <span className="block truncate text-xs font-medium">{name}</span>
                <span className="block truncate text-[var(--fs-tiny)] text-foreground/42">{ownerLabel || `${count} 个素材`}</span>
            </button>
            {menu ? <Dropdown trigger={["click"]} menu={menu}><button type="button" className="grid size-7 shrink-0 place-items-center rounded text-foreground/45 hover:bg-foreground/5 hover:text-foreground" aria-label={`管理文件夹：${name}`}><MoreHorizontal className="size-3.5" /></button></Dropdown> : <span className="text-[var(--fs-tiny)] tabular-nums text-foreground/35">{count}</span>}
        </div>
    );
    return menu ? <Dropdown trigger={["contextMenu"]} menu={menu}><div>{content}</div></Dropdown> : content;
}
