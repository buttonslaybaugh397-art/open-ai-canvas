const volcengineAssetIdPattern = /^asset-[A-Za-z0-9][A-Za-z0-9._-]*$/;

type AssetRecordLike = { id?: unknown; assetId?: unknown; assetUri?: unknown; providerAssetId?: unknown; providerAssetUri?: unknown; data?: unknown; metadata?: unknown; coverUrl?: unknown; volcengineAssetUri?: unknown; volcengineAssetId?: unknown; arkAssetUri?: unknown; arkAssetId?: unknown };

export function normalizeVolcengineAssetUri(value: unknown): string | undefined {
    const normalized = typeof value === "string" ? value.trim() : "";
    if (!normalized) return undefined;
    if (normalized.startsWith("asset://")) {
        const assetId = normalized.slice("asset://".length);
        return volcengineAssetIdPattern.test(assetId) ? `asset://${assetId}` : undefined;
    }
    return volcengineAssetIdPattern.test(normalized) ? `asset://${normalized}` : undefined;
}

export function firstVolcengineAssetUri(...values: unknown[]) {
    for (const value of values) {
        const uri = normalizeVolcengineAssetUri(value);
        if (uri) return uri;
    }
    return undefined;
}

export function volcengineAssetUriForAsset(asset: AssetRecordLike) {
    const data = recordValue(asset.data);
    const metadata = recordValue(asset.metadata);
    return firstVolcengineAssetUri(
        asset.volcengineAssetUri,
        asset.volcengineAssetId,
        asset.assetUri,
        asset.assetId,
        asset.providerAssetUri,
        asset.providerAssetId,
        asset.arkAssetUri,
        asset.arkAssetId,
        asset.id,
        data?.volcengineAssetUri,
        data?.volcengineAssetId,
        data?.assetId,
        data?.asset_id,
        data?.volcengine_asset_id,
        data?.volcengineAssetID,
        data?.assetUri,
        data?.asset_uri,
        data?.providerAssetUri,
        data?.providerAssetId,
        data?.arkAssetUri,
        data?.arkAssetId,
        data?.ark_asset_id,
        metadata?.volcengineAssetUri,
        metadata?.volcengineAssetId,
        metadata?.assetId,
        metadata?.asset_id,
        metadata?.volcengine_asset_id,
        metadata?.volcengineAssetID,
        metadata?.assetUri,
        metadata?.asset_uri,
        metadata?.providerAssetUri,
        metadata?.providerAssetId,
        metadata?.arkAssetUri,
        metadata?.arkAssetId,
        metadata?.ark_asset_id,
        asset.coverUrl,
        data?.dataUrl,
        data?.url,
    );
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
    return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
}
