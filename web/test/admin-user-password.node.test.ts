import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { adminPasswordError, generateAdminPassword } from "../src/pages/admin/users/users-password.ts";

test("admin generated passwords use cryptographic randomness and satisfy the password contract", () => {
    const passwords = new Set(Array.from({ length: 32 }, generateAdminPassword));
    assert.equal(passwords.size, 32);
    for (const password of passwords) {
        assert.match(password, /^[A-Za-z0-9_-]{24}$/);
        assert.equal(adminPasswordError(password), undefined);
    }
});

test("admin password validation counts Unicode characters and the bcrypt byte limit", () => {
    for (const password of ["", "1234567", "a".repeat(73), "密".repeat(25)]) {
        assert.ok(adminPasswordError(password));
    }
    for (const password of ["12345678", "a".repeat(72), "密".repeat(24), " new-password "]) {
        assert.equal(adminPasswordError(password), undefined);
    }
});

test("admin password modal sends only the password and clears the secret after completion", () => {
    const source = readFileSync(new URL("../src/pages/admin/users/users-password-modal.tsx", import.meta.url), "utf8");
    const columns = readFileSync(new URL("../src/pages/admin/users/users-columns.tsx", import.meta.url), "utf8");
    const panel = readFileSync(new URL("../src/pages/admin/users/users-panel.tsx", import.meta.url), "utf8");
    assert.ok(source.includes("await updateAdminUser(user.id, { password: values.password })"));
    assert.ok(source.includes('dependencies={["password"]}'));
    assert.ok(source.includes("setSavedPassword(values.password)"));
    assert.ok(source.includes('setSavedPassword("")'));
    assert.ok(source.includes("if (editingSelf) void onSelfReset()"));
    assert.ok(source.includes("navigator.clipboard.writeText(savedPassword)"));
    assert.ok(!source.includes("localStorage"));
    assert.ok(!source.includes("sessionStorage"));
    assert.ok(!source.includes("Math.random"));
    assert.ok(columns.includes('key: "reset-password"'));
    assert.ok(panel.includes("<AdminUserPasswordModal"));
    assert.ok(panel.includes('window.location.replace("/login?next=%2Fadmin%2Fusers")'));
});
