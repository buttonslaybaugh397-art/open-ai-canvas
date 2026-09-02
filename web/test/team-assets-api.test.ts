import { afterEach, describe, expect, test } from "bun:test";

import { apiClient } from "../src/services/api/request";
import { acceptTeamInvitation, addTeamMember, createTeamInvitation, getTeam, getTeamInvitation, importTeamAssets, listTeamAssets, listTeamAuditEvents, listTeamInvitations, listTeamMembers, removeTeamMember, revokeTeamInvitation, shareTeamAssets, updateTeam, updateTeamMemberRole } from "../src/services/api/team-assets";

const originalAdapter = apiClient.defaults.adapter;

afterEach(() => {
    apiClient.defaults.adapter = originalAdapter;
});

describe("team asset API contracts", () => {
    test("scopes list requests to a team and sends server filters", async () => {
        let captured: { url?: string; params?: unknown } = {};
        apiClient.defaults.adapter = async (config) => {
            captured = { url: config.url, params: config.params };
            return {
                data: { code: 0, data: { assets: [], page: 2, pageSize: 35, total: 0 }, msg: "ok" },
                status: 200,
                statusText: "OK",
                headers: {},
                config,
            };
        };

        await listTeamAssets("team / 1", { page: 2, pageSize: 35, query: "角色", kind: "image", folderId: "folder-1" });

        expect(captured.url).toBe("/teams/team%20%2F%201/assets");
        expect(captured.params).toEqual({ page: 2, pageSize: 35, query: "角色", kind: "image", folderId: "folder-1" });
    });

    test("shares only personal asset ids instead of client asset records", async () => {
        let captured: { url?: string; data?: string } = {};
        apiClient.defaults.adapter = async (config) => {
            captured = { url: config.url, data: config.data };
            return {
                data: { code: 0, data: { assets: [] }, msg: "ok" },
                status: 200,
                statusText: "OK",
                headers: {},
                config,
            };
        };

        await shareTeamAssets("team-1", ["asset-1", "asset-2"]);

        expect(captured.url).toBe("/teams/team-1/assets/share");
        expect(JSON.parse(captured.data || "{}")).toEqual({ assetIds: ["asset-1", "asset-2"] });
    });

    test("imports team asset ids into independent personal copies", async () => {
        let captured: { url?: string; data?: string } = {};
        apiClient.defaults.adapter = async (config) => {
            captured = { url: config.url, data: config.data };
            return {
                data: { code: 0, data: { imported: [] }, msg: "ok" },
                status: 200,
                statusText: "OK",
                headers: {},
                config,
            };
        };

        await importTeamAssets("team / 1", ["team-asset-1", "team-asset-2"]);

        expect(captured.url).toBe("/teams/team%20%2F%201/assets/import");
        expect(JSON.parse(captured.data || "{}")).toEqual({ assetIds: ["team-asset-1", "team-asset-2"] });
    });

    test("uses scoped member-management routes and minimal payloads", async () => {
        const captured: Array<{ method?: string; url?: string; data?: string }> = [];
        apiClient.defaults.adapter = async (config) => {
            captured.push({ method: config.method, url: config.url, data: config.data });
            return {
                data: { code: 0, data: config.method === "get" ? { members: [] } : config.method === "delete" ? { userId: "user / 1" } : { member: {} }, msg: "ok" },
                status: 200,
                statusText: "OK",
                headers: {},
                config,
            };
        };

        await listTeamMembers("team / 1");
        await addTeamMember("team / 1", { username: "creator", role: "viewer" });
        await updateTeamMemberRole("team / 1", "user / 1", "editor");
        await removeTeamMember("team / 1", "user / 1");

        expect(captured.map(({ method, url }) => ({ method, url }))).toEqual([
            { method: "get", url: "/teams/team%20%2F%201/members" },
            { method: "post", url: "/teams/team%20%2F%201/members" },
            { method: "patch", url: "/teams/team%20%2F%201/members/user%20%2F%201" },
            { method: "delete", url: "/teams/team%20%2F%201/members/user%20%2F%201" },
        ]);
        expect(JSON.parse(captured[1]?.data || "{}")).toEqual({ username: "creator", role: "viewer" });
        expect(JSON.parse(captured[2]?.data || "{}")).toEqual({ role: "editor" });
    });

    test("reads and updates only supported team settings", async () => {
        const captured: Array<{ method?: string; url?: string; data?: string }> = [];
        apiClient.defaults.adapter = async (config) => {
            captured.push({ method: config.method, url: config.url, data: config.data });
            return {
                data: { code: 0, data: { team: {}, usage: {} }, msg: "ok" },
                status: 200,
                statusText: "OK",
                headers: {},
                config,
            };
        };

        await getTeam("team / 1");
        await updateTeam("team / 1", { name: "Studio", description: "Shared assets", assetLimit: 500, storageLimitBytes: 20 * 1024 ** 3 });

        expect(captured.map(({ method, url }) => ({ method, url }))).toEqual([
            { method: "get", url: "/teams/team%20%2F%201" },
            { method: "patch", url: "/teams/team%20%2F%201" },
        ]);
        expect(JSON.parse(captured[1]?.data || "{}")).toEqual({ name: "Studio", description: "Shared assets", assetLimit: 500, storageLimitBytes: 20 * 1024 ** 3 });
    });

    test("reads paged audit events from the scoped team route", async () => {
        let captured: { url?: string; params?: unknown } = {};
        apiClient.defaults.adapter = async (config) => {
            captured = { url: config.url, params: config.params };
            return {
                data: { code: 0, data: { events: [], page: 3, pageSize: 10, total: 0 }, msg: "ok" },
                status: 200,
                statusText: "OK",
                headers: {},
                config,
            };
        };

        await listTeamAuditEvents("team / 1", 3, 10);

        expect(captured).toEqual({ url: "/teams/team%20%2F%201/audit-events", params: { page: 3, pageSize: 10 } });
    });

    test("uses scoped invitation lifecycle routes and minimal payloads", async () => {
        const captured: Array<{ method?: string; url?: string; data?: string }> = [];
        apiClient.defaults.adapter = async (config) => {
            captured.push({ method: config.method, url: config.url, data: config.data });
            const data = config.method === "get" && config.url?.includes("team-invitations/")
                ? { teamName: "Studio", role: "editor", expiresAt: "2026-09-02T00:00:00Z", available: true }
                : config.method === "get"
                    ? { invitations: [] }
                    : config.url?.endsWith("/accept")
                        ? { team: { id: "team-1" } }
                        : config.method === "delete"
                            ? { id: "invite / 1" }
                            : { invitation: {}, inviteUrl: "/teams/join/token" };
            return { data: { code: 0, data, msg: "ok" }, status: 200, statusText: "OK", headers: {}, config };
        };

        await listTeamInvitations("team / 1");
        await createTeamInvitation("team / 1", { role: "editor", validHours: 72 });
        await revokeTeamInvitation("team / 1", "invite / 1");
        await getTeamInvitation("token / 1");
        await acceptTeamInvitation("token / 1");

        expect(captured.map(({ method, url }) => ({ method, url }))).toEqual([
            { method: "get", url: "/teams/team%20%2F%201/invitations" },
            { method: "post", url: "/teams/team%20%2F%201/invitations" },
            { method: "delete", url: "/teams/team%20%2F%201/invitations/invite%20%2F%201" },
            { method: "get", url: "/team-invitations/token%20%2F%201" },
            { method: "post", url: "/team-invitations/token%20%2F%201/accept" },
        ]);
        expect(JSON.parse(captured[1]?.data || "{}")).toEqual({ role: "editor", validHours: 72 });
    });
});
