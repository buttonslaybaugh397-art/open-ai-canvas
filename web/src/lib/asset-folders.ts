import type { Asset, EntityAsset, NewAsset } from "@/stores/use-asset-store";

const ASSET_FOLDER_TYPE = "asset-folder";

export type PersonalAssetFolder = {
    id: string;
    name: string;
    createdAt: string;
    updatedAt: string;
};

export function isAssetFolderEntity(asset: Asset): asset is EntityAsset {
    return asset.kind === "entity" && asset.data.definition.type === ASSET_FOLDER_TYPE;
}

export function personalAssetFolders(assets: Asset[]): PersonalAssetFolder[] {
    return assets
        .filter(isAssetFolderEntity)
        .map((asset) => ({ id: asset.id, name: String(asset.data.definition.name || asset.title).trim() || "未命名文件夹", createdAt: asset.createdAt, updatedAt: asset.updatedAt }))
        .sort((left, right) => left.name.localeCompare(right.name, "zh-CN"));
}

export function newPersonalAssetFolder(name: string): NewAsset {
    const normalized = name.trim();
    return {
        kind: "entity",
        title: normalized,
        coverUrl: "",
        tags: [],
        category: "other",
        source: "素材库文件夹",
        data: { definition: { type: ASSET_FOLDER_TYPE, name: normalized } },
    };
}

export function renamedPersonalAssetFolder(asset: EntityAsset, name: string) {
    const normalized = name.trim();
    return { title: normalized, data: { definition: { ...asset.data.definition, type: ASSET_FOLDER_TYPE, name: normalized } } };
}
