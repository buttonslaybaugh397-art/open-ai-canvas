import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, test } from "bun:test";

import { adminResourceFileUrl } from "@/services/api/admin-storage";

const source = readFileSync(resolve(import.meta.dir, "../src/pages/admin/components/storage-resources-panel.tsx"), "utf8");
const apiSource = readFileSync(resolve(import.meta.dir, "../src/services/api/admin-storage.ts"), "utf8");

describe("admin storage CDN delivery", () => {
    test("preview keeps the admin-authenticated resource endpoint without forcing a proxy", () => {
        const url = new URL(adminResourceFileUrl("resource/a"), "http://localhost");
        expect(url.pathname).toEndWith("/admin/resources/resource%2Fa/file");
        expect(url.search).toBe("");
        expect(source).toContain('cacheResourceObjectUrl(`resource:${resource.id}`, "admin")');
    });

    test("download requests direct delivery and encodes the attachment name", () => {
        const name = "\u7ec8\u7a3f video #1 & final.mp4";
        const url = new URL(adminResourceFileUrl("resource/a", true, name), "http://localhost");
        expect(url.pathname).toEndWith("/admin/resources/resource%2Fa/file");
        expect(url.searchParams.get("direct")).toBe("1");
        expect(url.searchParams.get("download")).toBe("1");
        expect(url.searchParams.get("filename")).toBe(name);
        expect(url.searchParams.has("proxy")).toBe(false);
        expect(url.hash).toBe("");
    });

    test("empty names are omitted for the backend fallback", () => {
        for (const name of [undefined, "", "   "]) {
            const url = new URL(adminResourceFileUrl("video-1", true, name), "http://localhost");
            expect(url.searchParams.has("filename")).toBe(false);
        }
    });

    test("both download buttons isolate CDN navigation away from the admin page", () => {
        expect(source).toContain("onClick={() => downloadResource(resource)}");
        expect(source).toContain("onClick={() => downloadResource(previewing)}");
        expect(source).toContain('downloadResourceFile(`resource:${resource.id}`, name,');
        expect(source).not.toContain("href={adminResourceFileUrl(");
        expect(source).toContain('disabled={resource.status !== "ready"}');
        expect(source).toContain('disabled={previewing.status !== "ready"}');
        expect(source).not.toContain("downloadAdminResource");
        expect(source).not.toContain("saveAs");
        expect(apiSource).not.toContain("fetch(");
        expect(apiSource).not.toContain(".blob()");
    });
});
