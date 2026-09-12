import { expect, test } from "bun:test";

import { getAuthSession, logout, updateOwnDisplayName, type AuthSessionPayload, type LocalUser } from "../src/services/api/auth";
import { apiClient, type BackendEnvelope } from "../src/services/api/request";
import { useUserStore } from "../src/stores/use-user-store";

const user: LocalUser = {
    id: "test-user",
    username: "original",
    displayName: "Display",
    role: "user",
    status: "active",
    avatarUrl: "https://example.com/avatar.png",
    identityProvider: "linuxdo",
    identityId: "subject",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
};

test("self rename sends only displayName, invalidates session cache and discards stale reads", async () => {
    const original = apiClient.request;
    const calls: { url?: string; data?: unknown }[] = [];
    let currentUser = { ...user };
    let rejected = false;
    let resolveStale: ((value: { data: BackendEnvelope<AuthSessionPayload>; status: number }) => void) | undefined;
    let holdSession = false;
    apiClient.request = (async (config: { url?: string; data?: { displayName?: string } }) => {
        calls.push(config);
        if (config.url === "/auth/logout") return { data: { code: 0, data: { ok: true } }, status: 200 };
        if (config.url === "/auth/display-name") {
            if (rejected) return { data: { code: 409, data: null, msg: "显示名称已变化", reason: "conflict" }, status: 409 };
            currentUser = { ...currentUser, displayName: config.data!.displayName! };
        }
        if (config.url === "/auth/session" && holdSession) {
            holdSession = false;
            return new Promise((resolve) => {
                resolveStale = resolve;
            });
        }
        return { data: { code: 0, data: { user: { ...currentUser } } }, status: 200 };
    }) as typeof apiClient.request;
    try {
        await logout();
        expect((await getAuthSession()).user?.username).toBe("original");
        const unsafeInput = { displayName: "中文名称", username: "must-not-change", role: "admin", id: "another-user" };
        const renamed = (await updateOwnDisplayName(unsafeInput)).user;
        expect(renamed.displayName).toBe("中文名称");
        expect(renamed.username).toBe(user.username);
        expect(calls.find((call) => call.url === "/auth/display-name")?.data).toEqual({ displayName: "中文名称" });
        expect((await getAuthSession()).user?.displayName).toBe("中文名称");
        await logout();
        holdSession = true;
        const stale = getAuthSession();
        await updateOwnDisplayName({ displayName: "最新名称" });
        resolveStale!({ data: { code: 0, data: { user }, msg: "ok" }, status: 200 });
        expect((await stale).user?.displayName).toBe("最新名称");
        expect((await getAuthSession()).user?.displayName).toBe("最新名称");
        rejected = true;
        await expect(updateOwnDisplayName({ displayName: "过时名称" })).rejects.toMatchObject({ code: 409, reason: "conflict" });
        expect((await getAuthSession()).user?.displayName).toBe("最新名称");
    } finally {
        await logout();
        apiClient.request = original;
    }
});

test("display name update preserves login and session metadata and ignores another account's late response", () => {
    const previous = useUserStore.getState();
    try {
        useUserStore.getState().setUser(user);
        const features = useUserStore.getState().features;
        const limits = useUserStore.getState().runtimeLimits;
        useUserStore.getState().updateDisplayName({ id: user.id, displayName: "新名字", updatedAt: "2026-02-01T00:00:00Z" });
        expect(useUserStore.getState().user).toEqual({ ...user, displayName: "新名字", updatedAt: "2026-02-01T00:00:00Z" });
        expect(useUserStore.getState().features).toBe(features);
        expect(useUserStore.getState().runtimeLimits).toBe(limits);
        useUserStore.getState().setUser({ ...user, id: "second-user" });
        useUserStore.getState().updateDisplayName({ id: user.id, displayName: "过时名字" });
        expect(useUserStore.getState().user?.displayName).toBe(user.displayName);
        useUserStore.getState().clearSession();
        useUserStore.getState().updateDisplayName({ id: user.id, displayName: "过时名字" });
        expect(useUserStore.getState().user).toBeNull();
    } finally {
        useUserStore.setState(previous);
    }
});

test("account settings are reachable from both menus and submit the dedicated self endpoint", async () => {
    const [pane, page, topMenu, sidebar] = await Promise.all([
        Bun.file(new URL("../src/pages/settings/account-settings-pane.tsx", import.meta.url)).text(),
        Bun.file(new URL("../src/pages/settings/index.tsx", import.meta.url)).text(),
        Bun.file(new URL("../src/components/layout/workspace-account-menu.tsx", import.meta.url)).text(),
        Bun.file(new URL("../src/components/layout/workspace-sidebar-footer.tsx", import.meta.url)).text(),
    ]);
    expect(page).toContain("account: <SettingsPane><AccountSettingsPane /></SettingsPane>");
    for (const source of [topMenu, sidebar]) expect(source).toContain("/settings?section=account");
    expect(pane).toContain("await updateOwnDisplayName({ displayName: values.displayName.trim() })");
    expect(pane).toMatch(/name="displayName"\s+label="显示名称"/);
    expect(pane).toContain("Array.from(name).length > 40");
    expect(pane).not.toContain('name="username"');
    expect(pane).not.toContain("updateOwnUsername");
    expect(pane).toContain("loading={saving}");
    expect(pane).toContain("取消修改");
    expect(pane).not.toContain("updateAdminUser");
    expect(pane).not.toContain("applyUserSession");
});
