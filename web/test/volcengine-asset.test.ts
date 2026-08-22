import { firstVolcengineAssetUri, normalizeVolcengineAssetUri, volcengineAssetUriForAsset } from "../src/lib/volcengine-asset";

test("normalizes Volcengine Asset IDs without treating URLs as Assets", () => {
    expect(normalizeVolcengineAssetUri("asset-20260822124413-4qzzj")).toBe("asset://asset-20260822124413-4qzzj");
    expect(normalizeVolcengineAssetUri("asset://asset-20260822124413-4qzzj")).toBe("asset://asset-20260822124413-4qzzj");
    expect(normalizeVolcengineAssetUri("https://example.com/asset-20260822124413-4qzzj.png")).toBeUndefined();
    expect(firstVolcengineAssetUri("https://example.com/reference.png", "asset-20260822124413-4qzzj")).toBe("asset://asset-20260822124413-4qzzj");
});

test("reads explicit Volcengine Asset fields before preview URLs", () => {
    expect(volcengineAssetUriForAsset({
        coverUrl: "https://example.com/preview.png",
        data: { dataUrl: "https://example.com/image.png", providerAssetId: "asset-20260822124413-4qzzj" },
    })).toBe("asset://asset-20260822124413-4qzzj");
    expect(volcengineAssetUriForAsset({
        coverUrl: "https://example.com/preview.png",
        data: { dataUrl: "https://example.com/image.png" },
    })).toBeUndefined();
});

test("recognizes an imported Volcengine Asset ID stored as the asset record ID", () => {
    expect(volcengineAssetUriForAsset({
        id: "asset-20260822124413-4qzzj",
        coverUrl: "https://example.com/preview.png",
        data: { dataUrl: "https://example.com/image.png" },
    })).toBe("asset://asset-20260822124413-4qzzj");
});

test("prefers an imported Asset ID over a preview URL", () => {
    expect(firstVolcengineAssetUri(
        "",
        "asset-20260822124413-4qzzj",
        "https://example.com/reference.png",
    )).toBe("asset://asset-20260822124413-4qzzj");
});
