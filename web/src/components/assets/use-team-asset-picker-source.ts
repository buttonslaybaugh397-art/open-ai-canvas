import { useQuery } from "@tanstack/react-query";

import type { AssetLibraryPickerFolder, AssetLibraryPickerItem } from "@/components/assets/asset-library-picker-modal";
import { importTeamAssets, listTeamAssetFolders, listTeamAssets, listTeams, type TeamSummary } from "@/services/api/team-assets";
import { useAssetStore, type Asset, type AssetKind } from "@/stores/use-asset-store";
import { useUserStore } from "@/stores/use-user-store";

const TEAM_PICKER_ID_PREFIX = "team:";

export type TeamAssetPickerKind = Exclude<AssetKind, "entity">;

export function teamAssetPickerId(teamId: string, teamAssetId: string) {
    return `${TEAM_PICKER_ID_PREFIX}${encodeURIComponent(teamId)}:${encodeURIComponent(teamAssetId)}`;
}

export function parseTeamAssetPickerId(id: string) {
    if (!id.startsWith(TEAM_PICKER_ID_PREFIX)) return null;
    const separator = id.indexOf(":", TEAM_PICKER_ID_PREFIX.length);
    if (separator < 0) return null;
    try {
        return {
            teamId: decodeURIComponent(id.slice(TEAM_PICKER_ID_PREFIX.length, separator)),
            teamAssetId: decodeURIComponent(id.slice(separator + 1)),
        };
    } catch {
        return null;
    }
}

export async function materializeTeamAssetSelection(
    ids: string[],
    importer: typeof importTeamAssets = importTeamAssets,
): Promise<{ ids: string[]; assets: Asset[] }> {
    const groups = new Map<string, string[]>();
    for (const id of ids) {
        const parsed = parseTeamAssetPickerId(id);
        if (!parsed) continue;
        const group = groups.get(parsed.teamId) || [];
        if (!group.includes(parsed.teamAssetId)) group.push(parsed.teamAssetId);
        groups.set(parsed.teamId, group);
    }
    if (!groups.size) return { ids, assets: [] };

    const importedByPickerId = new Map<string, Asset>();
    for (const [teamId, teamAssetIds] of groups) {
        const result = await importer(teamId, teamAssetIds);
        for (const item of result.imported) {
            importedByPickerId.set(teamAssetPickerId(teamId, item.teamAssetId), item.asset);
        }
    }

    const missing = ids.filter((id) => parseTeamAssetPickerId(id) && !importedByPickerId.has(id));
    if (missing.length) throw new Error(`有 ${missing.length} 个团队素材未能复制，请刷新后重试`);

    const assets = ids.flatMap((id) => {
        const asset = importedByPickerId.get(id);
        return asset ? [asset] : [];
    });
    mergeImportedAssets(assets);
    return {
        ids: ids.map((id) => importedByPickerId.get(id)?.id || id),
        assets,
    };
}

export function mergeImportedAssets(imported: Asset[]) {
    if (!imported.length) return;
    const current = useAssetStore.getState().assets;
    const importedIds = new Set(imported.map((asset) => asset.id));
    useAssetStore.getState().replaceAssets([...imported, ...current.filter((asset) => !importedIds.has(asset.id))]);
}

export function useTeamAssetPickerSource(input: {
    open: boolean;
    active: boolean;
    teamId: string;
    page: number;
    pageSize: number;
    keyword: string;
    kind?: TeamAssetPickerKind;
    folderId?: string;
    allowedKinds: TeamAssetPickerKind[];
}) {
    const userId = useUserStore((state) => state.user?.id || "");
    const teamsQuery = useQuery({
        queryKey: ["team-asset-picker-teams", userId],
        queryFn: ({ signal }) => listTeams(signal),
        enabled: input.open && Boolean(userId),
        staleTime: 30_000,
    });
    const assetsQuery = useQuery({
        queryKey: ["team-asset-picker-assets", userId, input.teamId, input.page, input.pageSize, input.keyword, input.kind, input.folderId],
        queryFn: ({ signal }) => listTeamAssets(input.teamId, {
            page: input.page,
            pageSize: input.pageSize,
            query: input.keyword || undefined,
            kind: input.kind,
            folderId: input.folderId,
            signal,
        }),
        enabled: input.open && input.active && Boolean(userId && input.teamId),
        placeholderData: (previous) => previous,
        staleTime: 15_000,
    });
    const foldersQuery = useQuery({
        queryKey: ["team-asset-picker-folders", userId, input.teamId],
        queryFn: ({ signal }) => listTeamAssetFolders(input.teamId, signal),
        enabled: input.open && input.active && Boolean(userId && input.teamId),
        staleTime: 30_000,
    });
    const allowed = new Set<AssetKind>(input.allowedKinds);
    const items: AssetLibraryPickerItem[] = (assetsQuery.data?.assets || []).flatMap((item) => {
        if (!allowed.has(item.asset.kind) || item.asset.kind === "entity") return [];
        return [{
            id: teamAssetPickerId(input.teamId, item.id),
            title: item.asset.title,
            category: item.asset.kind,
            kindLabel: kindLabel(item.asset.kind),
            asset: item.asset,
            folderId: item.folderId,
            description: `${item.owner.displayName || item.owner.username} · 团队素材`,
            searchText: (item.asset.tags || []).join(" "),
        }];
    });
    const folders: AssetLibraryPickerFolder[] = (foldersQuery.data?.folders || []).map((folder) => ({ id: folder.id, name: folder.name }));
    return {
        teams: teamsQuery.data?.teams || [] as TeamSummary[],
        teamsLoading: teamsQuery.isLoading,
        items,
        folders,
        loading: assetsQuery.isLoading || assetsQuery.isFetching,
        total: assetsQuery.data?.total || 0,
        error: readError(teamsQuery.error || assetsQuery.error || foldersQuery.error),
    };
}

function kindLabel(kind: AssetKind) {
    if (kind === "image") return "图片";
    if (kind === "video") return "视频";
    if (kind === "audio") return "音频";
    if (kind === "text") return "文本";
    return "3D 模型";
}

function readError(error: unknown) {
    return error instanceof Error ? error.message : error ? "团队素材读取失败" : "";
}
