import type { Asset, AssetKind } from "@/stores/use-asset-store";
import { apiClient, compactApiParams, request } from "@/services/api/request";

export type TeamRole = "owner" | "admin" | "editor" | "viewer";

export type TeamSummary = {
    id: string;
    name: string;
    description?: string;
    role: TeamRole;
    canEdit?: boolean;
    canManage?: boolean;
    memberCount?: number;
    createdAt: string;
    updatedAt: string;
};

export type TeamUsage = {
    memberCount: number;
    assetCount: number;
    assetLimit: number;
    storageBytes: number;
    storageLimitBytes: number;
};

export type TeamDetail = { team: TeamSummary; usage: TeamUsage };

export type TeamMember = {
    userId: string;
    username: string;
    displayName: string;
    role: TeamRole;
    joinedAt: string;
};

export type TeamAuditEvent = {
    id: string;
    action: string;
    targetType?: string;
    targetId?: string;
    summary: string;
    actorUserId: string;
    actorUsername: string;
    actorDisplayName: string;
    createdAt: string;
};

export type TeamAuditPage = {
    events: TeamAuditEvent[];
    page: number;
    pageSize: number;
    total: number;
};

export type TeamInvitation = {
    id: string;
    teamId: string;
    role: Exclude<TeamRole, "owner">;
    expiresAt: string;
    createdAt: string;
};

export type TeamInvitationPreview = {
    teamName: string;
    role: Exclude<TeamRole, "owner">;
    expiresAt: string;
    available: boolean;
};

export type TeamAssetOwner = {
    id: string;
    username: string;
    displayName: string;
};

export type TeamAssetItem = {
    id: string;
    sourceAssetId: string;
    folderId?: string;
    asset: Asset & { folderId?: string };
    owner: TeamAssetOwner;
    canEdit: boolean;
    createdAt: string;
    updatedAt: string;
};

export type TeamAssetFolder = {
    id: string;
    name: string;
    count?: number;
    canEdit: boolean;
    createdAt: string;
    updatedAt: string;
};

export type TeamAssetListParams = {
    page: number;
    pageSize: number;
    query?: string;
    kind?: Exclude<AssetKind, "entity">;
    folderId?: string;
    signal?: AbortSignal;
};

export type TeamAssetPage = {
    assets: TeamAssetItem[];
    page: number;
    pageSize: number;
    total: number;
};

export type ShareTeamAssetsResult = {
    assets: TeamAssetItem[];
    skipped?: Array<{ assetId: string; reason: string }>;
};

export type ImportTeamAssetsResult = {
    imported: Array<{ teamAssetId: string; asset: Asset }>;
};

export function canWriteTeamAssets(role?: TeamRole) {
    return role === "owner" || role === "admin" || role === "editor";
}

export function listTeams(signal?: AbortSignal) {
    return request<{ teams: TeamSummary[] }>(apiClient.get("/teams", { signal }));
}

export function createTeam(input: { name: string; description?: string }) {
    return request<{ team: TeamSummary }>(apiClient.post("/teams", input));
}

export function getTeam(teamId: string, signal?: AbortSignal) {
    return request<TeamDetail>(apiClient.get(`/teams/${encodeURIComponent(teamId)}`, { signal }));
}

export function updateTeam(teamId: string, input: { name: string; description: string; assetLimit: number; storageLimitBytes: number }) {
    return request<TeamDetail>(apiClient.patch(`/teams/${encodeURIComponent(teamId)}`, input));
}

export function listTeamMembers(teamId: string, signal?: AbortSignal) {
    return request<{ members: TeamMember[] }>(apiClient.get(`/teams/${encodeURIComponent(teamId)}/members`, { signal }));
}

export function listTeamAuditEvents(teamId: string, page: number, pageSize: number, signal?: AbortSignal) {
    return request<TeamAuditPage>(apiClient.get(`/teams/${encodeURIComponent(teamId)}/audit-events`, {
        params: { page, pageSize },
        signal,
    }));
}

export function listTeamInvitations(teamId: string, signal?: AbortSignal) {
    return request<{ invitations: TeamInvitation[] }>(apiClient.get(`/teams/${encodeURIComponent(teamId)}/invitations`, { signal }));
}

export function createTeamInvitation(teamId: string, input: { role: Exclude<TeamRole, "owner">; validHours: 24 | 72 | 168 | 720 }) {
    return request<{ invitation: TeamInvitation; inviteUrl: string }>(apiClient.post(`/teams/${encodeURIComponent(teamId)}/invitations`, input));
}

export function revokeTeamInvitation(teamId: string, invitationId: string) {
    return request<{ id: string }>(apiClient.delete(`/teams/${encodeURIComponent(teamId)}/invitations/${encodeURIComponent(invitationId)}`));
}

export function getTeamInvitation(token: string, signal?: AbortSignal) {
    return request<TeamInvitationPreview>(apiClient.get(`/team-invitations/${encodeURIComponent(token)}`, { signal }));
}

export function acceptTeamInvitation(token: string) {
    return request<{ team: TeamSummary }>(apiClient.post(`/team-invitations/${encodeURIComponent(token)}/accept`));
}

export function addTeamMember(teamId: string, input: { username: string; role: Exclude<TeamRole, "owner"> }) {
    return request<{ member: TeamMember }>(apiClient.post(`/teams/${encodeURIComponent(teamId)}/members`, input));
}

export function updateTeamMemberRole(teamId: string, userId: string, role: Exclude<TeamRole, "owner">) {
    return request<{ member: TeamMember }>(apiClient.patch(`/teams/${encodeURIComponent(teamId)}/members/${encodeURIComponent(userId)}`, { role }));
}

export function removeTeamMember(teamId: string, userId: string) {
    return request<{ userId: string }>(apiClient.delete(`/teams/${encodeURIComponent(teamId)}/members/${encodeURIComponent(userId)}`));
}

export function listTeamAssets(teamId: string, input: TeamAssetListParams) {
    const { signal, page, pageSize, query, kind, folderId } = input;
    const params = compactApiParams({ page, pageSize, query: query?.trim(), kind, folderId });
    if (folderId === "") params.folderId = "";
    return request<TeamAssetPage>(apiClient.get(`/teams/${encodeURIComponent(teamId)}/assets`, {
        params,
        signal,
    }));
}

export function shareTeamAssets(teamId: string, assetIds: string[]) {
    return request<ShareTeamAssetsResult>(apiClient.post(`/teams/${encodeURIComponent(teamId)}/assets/share`, { assetIds }));
}

export function importTeamAssets(teamId: string, assetIds: string[]) {
    return request<ImportTeamAssetsResult>(apiClient.post(`/teams/${encodeURIComponent(teamId)}/assets/import`, { assetIds }));
}

export function listTeamAssetFolders(teamId: string, signal?: AbortSignal) {
    return request<{ folders: TeamAssetFolder[] }>(apiClient.get(`/teams/${encodeURIComponent(teamId)}/asset-folders`, { signal }));
}

export function createTeamAssetFolder(teamId: string, name: string) {
    return request<{ folder: TeamAssetFolder }>(apiClient.post(`/teams/${encodeURIComponent(teamId)}/asset-folders`, { name }));
}

export function renameTeamAssetFolder(teamId: string, folderId: string, name: string) {
    return request<{ folder: TeamAssetFolder }>(apiClient.patch(`/teams/${encodeURIComponent(teamId)}/asset-folders/${encodeURIComponent(folderId)}`, { name }));
}

export function deleteTeamAssetFolder(teamId: string, folderId: string) {
    return request<{ id: string }>(apiClient.delete(`/teams/${encodeURIComponent(teamId)}/asset-folders/${encodeURIComponent(folderId)}`));
}

export function deleteTeamAsset(teamId: string, assetId: string) {
    return request<{ id: string }>(apiClient.delete(`/teams/${encodeURIComponent(teamId)}/assets/${encodeURIComponent(assetId)}`));
}

export function moveTeamAsset(teamId: string, assetId: string, folderId: string) {
    return request<{ asset: TeamAssetItem }>(apiClient.patch(`/teams/${encodeURIComponent(teamId)}/assets/${encodeURIComponent(assetId)}/folder`, { folderId }));
}
