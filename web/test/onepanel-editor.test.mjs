import { afterEach, expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createEditorCompose } from "../../1panel-deploy/render-editor.mjs";

const template = Bun.YAML.parse(readFileSync(new URL("../../docker-compose.1panel.yml", import.meta.url), "utf8"));
const volumeNames = { "backend-data": "existing-backend", "deployment-secrets": "existing-secrets", "postgres-data": "existing-postgres", "redis-data": "existing-redis" };
const temporaryDirectories = [];
afterEach(() => {
    for (const directory of temporaryDirectories.splice(0)) {
        const relative = path.relative(os.tmpdir(), directory);
        if (path.isAbsolute(relative) || relative.startsWith("..") || !relative.startsWith("canvas-editor-")) throw new Error("Invalid cleanup path");
        rmSync(directory, { recursive: true, force: true });
    }
});

function resolvedFixture() {
    return {
        name: "open-ai-canvas",
        services: {
            postgres: { environment: { POSTGRES_DB: "existing_db", POSTGRES_USER: "existing_user", CANVAS_POSTGRES_PASSWORD: "test-only-private-value" }, command: [...template.services.postgres.command] },
            redis: { image: "redis:7.4-alpine" },
            migrate: { image: "ghcr.io/example/backend:sha-test", pull_policy: "always", command: [...template.services.migrate.command] },
            backend: {
                image: "ghcr.io/example/backend:sha-test",
                pull_policy: "always",
                environment: { CANVAS_CORS_ORIGINS: "https://canvas.example.com", CANVAS_AUTO_MIGRATE: "false" },
                command: [...template.services.backend.command],
                depends_on: structuredClone(template.services.backend.depends_on),
            },
            web: { image: "ghcr.io/example/web:sha-test", pull_policy: "always", ports: [{ host_ip: "127.0.0.1", published: "6868", target: 3000 }] },
        },
        volumes: Object.fromEntries(Object.entries(volumeNames).map(([key, name]) => [key, { name }])),
    };
}

test("editor output preserves deployment settings without embedding the password or creating empty volumes", () => {
    const input = resolvedFixture();
    const output = createEditorCompose(input);
    expect(input.services.postgres.environment.CANVAS_POSTGRES_PASSWORD).toBe("test-only-private-value");
    expect(output.services.postgres.environment.CANVAS_POSTGRES_PASSWORD).toBe("");
    expect(JSON.stringify(output)).not.toContain("test-only-private-value");
    expect(output.services.postgres.environment.POSTGRES_DB).toBe("existing_db");
    expect(output.services.backend).toEqual(input.services.backend);
    expect(output.services.migrate).toEqual(input.services.migrate);
    expect(output.services.web).toEqual(input.services.web);
    for (const [key, name] of Object.entries(volumeNames)) expect(output.volumes[key]).toEqual({ name, external: true });
    expect(output.services.postgres.command[0]).toStartWith("[ -s /run/canvas-secrets/postgres-password ] ||");
    expect(output.services.postgres.command[0]).toContain("exit 1; }");
    expect(output.services.postgres.command[0]).toEndWith(input.services.postgres.command[0]);
    expect(output.services.backend.command[2]).toContain('DATABASE_URL="$$(cat /run/canvas-secrets/database-url)"');
    expect(Bun.YAML.parse(Bun.YAML.stringify(output, null, 2))).toEqual(output);
});

test("editor can add a temporary Caddy port without replacing the legacy port", () => {
    const input = resolvedFixture();
    const output = createEditorCompose(input, { addHttpPort: "3000" });

    expect(output.services.web.ports).toHaveLength(2);
    expect(output.services.web.ports).toContainEqual({ host_ip: "127.0.0.1", published: "3000", target: 3000, protocol: "tcp" });
    expect(input.services.web.ports).toHaveLength(1);
});

test("editor rejects an invalid temporary Caddy port", () => {
    expect(() => createEditorCompose(resolvedFixture(), { addHttpPort: "not-a-port" })).toThrow("1 to 65535");
});

for (const origin of ["", "*", "https://canvas.example.com/", "https://canvas.example.com/api", "https://canvas.example.com?token=test", "https://user:pass@canvas.example.com", "https://*.example.com", "null"]) {
    test(`editor rejects invalid browser origin: ${origin}`, () => {
        const input = resolvedFixture();
        input.services.backend.environment.CANVAS_CORS_ORIGINS = origin;
        expect(() => createEditorCompose(input)).toThrow();
    });
}

for (const origin of ["https://canvas.example.com", "http://192.0.2.10:6868", "https://canvas.example.com,https://other.example.com", "http://[2001:db8::1]:6868"]) {
    test(`editor preserves exact browser origins: ${origin}`, () => {
        const input = resolvedFixture();
        input.services.backend.environment.CANVAS_CORS_ORIGINS = origin;
        expect(createEditorCompose(input).services.backend.environment.CANVAS_CORS_ORIGINS).toBe(origin);
    });
}

test("editor rejects unresolved variables, inconsistent images and missing existing volume names", () => {
    const unresolved = resolvedFixture();
    unresolved.services.web.image = "ghcr.io/example/web:${TAG:-latest}";
    expect(() => createEditorCompose(unresolved)).toThrow("Unresolved Compose variable");
    const unescaped = resolvedFixture();
    unescaped.services.backend.command[2] = 'export DATABASE_URL="$(cat /run/canvas-secrets/database-url)"\nexec server $DATABASE_URL';
    expect(() => createEditorCompose(unescaped)).toThrow("unescaped container shell variable");
    const mismatch = resolvedFixture();
    mismatch.services.migrate.image = "ghcr.io/example/backend:old";
    expect(() => createEditorCompose(mismatch)).toThrow("same image");
    const cachedImage = resolvedFixture();
    cachedImage.services.backend.pull_policy = "never";
    expect(() => createEditorCompose(cachedImage)).toThrow("pull_policy: always");
    const unnamedVolume = resolvedFixture();
    delete unnamedVolume.volumes["postgres-data"].name;
    expect(() => createEditorCompose(unnamedVolume)).toThrow("explicit existing volume name");
});

const composeAvailable = spawnSync("docker", ["compose", "version"], { timeout: 5000, stdio: "ignore" }).status === 0;
test.skipIf(!composeAvailable)("editor CLI produces standalone Compose and preserves private input without overwriting output", () => {
    const directory = mkdtempSync(path.join(os.tmpdir(), "canvas-editor-"));
    temporaryDirectories.push(directory);
    const envFile = path.join(directory, "private.env");
    const output = path.join(directory, "editor.yml");
    const secret = "TestOnlyPasswordNotForDeployment123456";
    const input = `CANVAS_CORS_ORIGINS=https://canvas.example.com\nPOSTGRES_PASSWORD=${secret}\nCANVAS_POSTGRES_VOLUME=expected-existing-postgres\nCANVAS_HTTP_PORT=6869\n`;
    writeFileSync(envFile, input);
    const args = [fileURLToPath(new URL("../../1panel-deploy/render-editor.mjs", import.meta.url)), "--env-file", envFile, "--output", output, "--image-tag", "sha-fixture"];
    const env = { ...process.env, CANVAS_CORS_ORIGINS: "https://wrong.example.com", CANVAS_POSTGRES_VOLUME: "wrong-volume", COMPOSE_PROJECT_NAME: "wrong-project" };
    const run = () => spawnSync(process.execPath, args, { encoding: "utf8", env, timeout: 30000 });
    const result = run();
    expect(result.status).toBe(0);
    const content = readFileSync(output, "utf8");
    expect(content + result.stdout + result.stderr).not.toContain(secret);
    expect(readFileSync(envFile, "utf8")).toBe(input);
    const document = Bun.YAML.parse(content);
    expect(document.name).toBe("open-ai-canvas");
    expect(document.services.backend.environment.CANVAS_CORS_ORIGINS).toBe("https://canvas.example.com");
    expect(document.volumes["postgres-data"]).toEqual({ name: "expected-existing-postgres", external: true });
    for (const name of ["migrate", "backend", "web"]) expect(document.services[name].image).toEndWith(":sha-fixture");
    const parsed = spawnSync("docker", ["compose", "--env-file", process.platform === "win32" ? "NUL" : "/dev/null", "-f", "-", "config", "--format", "json", "--no-normalize", "--no-path-resolution"], {
        input: content,
        encoding: "utf8",
        env: { ...env, COMPOSE_PROJECT_NAME: "", CANVAS_CORS_ORIGINS: "" },
        timeout: 30000,
    });
    expect(parsed.status).toBe(0);
    const resolved = JSON.parse(parsed.stdout);
    for (const name of ["postgres", "migrate", "backend"]) expect(resolved.services[name].command).toEqual(document.services[name].command);
    expect(resolved.services.backend.environment.CANVAS_CORS_ORIGINS).toBe("https://canvas.example.com");
    expect(run().status).not.toBe(0);
    expect(readFileSync(output, "utf8")).toBe(content);
});
