import type { Asset } from "@/stores/use-asset-store";
import { apiClient, request } from "@/services/api/request";

export type TeamAssetOwner = {
    id: string;
    username: string;
    displayName: string;
};

export type TeamAssetItem = {
    asset: Asset;
    owner: TeamAssetOwner;
    canEdit: boolean;
    createdAt: string;
    updatedAt: string;
};

export type TeamAssetFolder = {
    id: string;
    name: string;
    owner: TeamAssetOwner;
    canEdit: boolean;
    createdAt: string;
    updatedAt: string;
};

export function listTeamAssets() {
    return request<{ assets: TeamAssetItem[] }>(apiClient.get("/team-assets"));
}

export function listTeamAssetFolders() {
    return request<{ folders: TeamAssetFolder[] }>(apiClient.get("/team-assets/folders"));
}

export function createTeamAssetFolder(name: string) {
    return request<{ folder: TeamAssetFolder }>(apiClient.post("/team-assets/folders", { name }));
}

export function renameTeamAssetFolder(id: string, name: string) {
    return request<{ folder: TeamAssetFolder }>(apiClient.patch(`/team-assets/folders/${encodeURIComponent(id)}`, { name }));
}

export function deleteTeamAssetFolder(id: string) {
    return request<{ id: string }>(apiClient.delete(`/team-assets/folders/${encodeURIComponent(id)}`));
}

export function getTeamAsset(id: string) {
    return request<{ asset: TeamAssetItem }>(apiClient.get(`/team-assets/${encodeURIComponent(id)}`));
}

export function upsertTeamAsset(asset: Asset) {
    return request<{ asset: TeamAssetItem }>(apiClient.put(`/team-assets/${encodeURIComponent(asset.id)}`, { asset }));
}

export function deleteTeamAsset(id: string) {
    return request<{ id: string }>(apiClient.delete(`/team-assets/${encodeURIComponent(id)}`));
}

export function moveTeamAsset(id: string, folderId: string) {
    return request<{ asset: TeamAssetItem }>(apiClient.patch(`/team-assets/${encodeURIComponent(id)}/folder`, { folderId }));
}
