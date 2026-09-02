import { afterEach, describe, expect, test } from "bun:test";

import { materializeTeamAssetSelection, parseTeamAssetPickerId, teamAssetPickerId } from "../src/components/assets/use-team-asset-picker-source";
import { useAssetStore, type Asset } from "../src/stores/use-asset-store";

const originalAssets = useAssetStore.getState().assets;

afterEach(() => {
    useAssetStore.setState({ assets: originalAssets });
});

describe("team asset picker source", () => {
    test("round-trips encoded team and asset ids", () => {
        const id = teamAssetPickerId("team / 华东", "asset:角色 / 1");
        expect(parseTeamAssetPickerId(id)).toEqual({ teamId: "team / 华东", teamAssetId: "asset:角色 / 1" });
        expect(parseTeamAssetPickerId("personal-asset")).toBeNull();
    });

    test("imports each team separately and returns personal ids without replacing existing assets", async () => {
        const existing = imageAsset("personal-existing", "已有素材");
        const importedA = imageAsset("personal-a", "团队图片");
        const importedB = audioAsset("personal-b", "团队声音");
        useAssetStore.setState({ assets: [existing] });
        const calls: Array<{ teamId: string; ids: string[] }> = [];
        const first = teamAssetPickerId("team-a", "shared-a");
        const second = teamAssetPickerId("team-b", "shared-b");

        const result = await materializeTeamAssetSelection(["personal-existing", first, second], async (teamId, ids) => {
            calls.push({ teamId, ids });
            return { imported: teamId === "team-a"
                ? [{ teamAssetId: "shared-a", asset: importedA }]
                : [{ teamAssetId: "shared-b", asset: importedB }] };
        });

        expect(calls).toEqual([
            { teamId: "team-a", ids: ["shared-a"] },
            { teamId: "team-b", ids: ["shared-b"] },
        ]);
        expect(result.ids).toEqual(["personal-existing", "personal-a", "personal-b"]);
        expect(result.assets.map((asset) => asset.id)).toEqual(["personal-a", "personal-b"]);
        expect(useAssetStore.getState().assets.map((asset) => asset.id)).toEqual(["personal-a", "personal-b", "personal-existing"]);
    });

    test("rejects partial imports instead of passing stale team ids onward", async () => {
        const selected = teamAssetPickerId("team-a", "missing");
        await expect(materializeTeamAssetSelection([selected], async () => ({ imported: [] }))).rejects.toThrow("1 个团队素材未能复制");
    });
});

function imageAsset(id: string, title: string): Asset {
    return { id, title, kind: "image", coverUrl: "", tags: [], createdAt: "2026-09-01T00:00:00Z", updatedAt: "2026-09-01T00:00:00Z", data: { dataUrl: `/api/resources/${id}/file`, storageKey: `resource:${id}`, width: 100, height: 100, bytes: 10, mimeType: "image/png" } };
}

function audioAsset(id: string, title: string): Asset {
    return { id, title, kind: "audio", coverUrl: "", tags: [], createdAt: "2026-09-01T00:00:00Z", updatedAt: "2026-09-01T00:00:00Z", data: { url: `/api/resources/${id}/file`, storageKey: `resource:${id}`, bytes: 10, mimeType: "audio/mpeg" } };
}
