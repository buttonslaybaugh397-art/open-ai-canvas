import { afterEach, expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";

const read = (file) => readFileSync(new URL(`../../${file}`, import.meta.url), "utf8").replace(/\r\n/g, "\n");
const compose = Bun.YAML.parse(read("docker-compose.1panel.yml"));
const temporaryDirectories = [];
const bash = process.platform === "win32" ? path.resolve(path.dirname(Bun.which("git") || ""), "../bin/bash.exe") : "bash";
const shellPath = (value) => value.replaceAll("\\", "/").replace(/^([A-Za-z]):\//, (_, drive) => `/${drive.toLowerCase()}/`);

afterEach(() => {
    for (const directory of temporaryDirectories.splice(0)) {
        const relative = path.relative(os.tmpdir(), directory);
        if (path.isAbsolute(relative) || relative.startsWith("..") || !relative.startsWith("canvas-onepanel-")) throw new Error("Invalid test cleanup path");
        rmSync(directory, { recursive: true, force: true });
    }
});

test("standalone 1Panel deployment and root templates stay identical", () => {
    expect(read("1panel-deploy/docker-compose.yml")).toBe(read("docker-compose.1panel.yml"));
    expect(read("1panel-deploy/.env.example")).toBe(read("1panel.env.example"));
});

test("1Panel keeps fork images, explicit migration, readiness gates and existing volumes", () => {
    const { postgres, redis, migrate, backend, web } = compose.services;
    expect(compose.name).toBe("open-ai-canvas");
    expect(postgres.image).toBe("postgres:17-alpine");
    expect(redis.image).toBe("redis:7.4-alpine");
    for (const service of [migrate, backend, web]) {
        expect(service.image).toStartWith("ghcr.io/${CANVAS_IMAGE_OWNER:-buttonslaybaugh397-art}/open-ai-canvas-");
        expect(service.image).toEndWith(":${CANVAS_IMAGE_TAG:-latest}");
        expect(service.pull_policy).toBe("always");
    }
    expect(migrate.image).toBe(backend.image);
    expect(migrate.healthcheck.disable).toBe(true);
    expect(migrate.restart).toBe("no");
    expect(backend.environment.CANVAS_AUTO_MIGRATE).toBe("false");
    expect(backend.depends_on.migrate.condition).toBe("service_completed_successfully");
    expect(backend.depends_on.redis.condition).toBe("service_healthy");
    expect(web.depends_on.backend.condition).toBe("service_healthy");
    expect(backend.healthcheck.test.join(" ")).toContain("/api/health/ready");
    expect(backend.environment.CANVAS_CORS_ORIGINS).toBe("${CANVAS_CORS_ORIGINS:-}");
    expect(read("docker-compose.1panel.yml")).not.toMatch(/CANVAS_CORS_ORIGINS:\s*\$\{CANVAS_CORS_ORIGINS:\?/);
    expect(web.ports).toEqual(["${CANVAS_BIND_ADDRESS:-127.0.0.1}:${CANVAS_HTTP_PORT:-6868}:3000"]);
    for (const service of [postgres, redis, migrate, backend]) expect(service.ports).toBeUndefined();
    expect(backend.environment.CANVAS_UPDATER_SOCKET).toBeUndefined();
    expect(read("docker-compose.1panel.yml")).not.toContain("docker.sock");
    expect(compose.volumes).toEqual({
        "backend-data": { name: "${CANVAS_BACKEND_VOLUME:-open-ai-canvas_backend-data}" },
        "deployment-secrets": { name: "${CANVAS_SECRETS_VOLUME:-open-ai-canvas_deployment-secrets}" },
        "postgres-data": { name: "${CANVAS_POSTGRES_VOLUME:-open-ai-canvas_postgres-data}" },
        "redis-data": { name: "${CANVAS_REDIS_VOLUME:-open-ai-canvas_redis-data}" },
    });
});

function runCommand(service, overrides = {}) {
    const directory = mkdtempSync(path.join(os.tmpdir(), "canvas-onepanel-"));
    temporaryDirectories.push(directory);
    writeFileSync(path.join(directory, "migrate-schema"), '#!/bin/sh\nprintf "migration:%s\\n" "$1"\ncase "$1" in up) exit "$MOCK_UP_STATUS" ;; verify) exit "$MOCK_VERIFY_STATUS" ;; *) exit 99 ;; esac\n', { mode: 0o755 });
    writeFileSync(path.join(directory, "infinite-canvas-backend"), "#!/bin/sh\necho backend-started\n", { mode: 0o755 });
    const command = compose.services[service].command;
    expect(command.slice(0, 2)).toEqual(["/bin/sh", "-ec"]);
    const prelude = 'export PATH="$MOCK_BIN:$PATH"\ncat() { [ "$1" = /run/canvas-secrets/database-url ] || return 99; [ "$MOCK_FILE_STATUS" = 0 ] || return "$MOCK_FILE_STATUS"; printf "%s" "$MOCK_DSN"; }\n';
    const result = spawnSync(bash, ["--noprofile", "--norc", "-ec", prelude + command[2].replaceAll("$$", "$")], {
        encoding: "utf8",
        env: { ...process.env, MOCK_BIN: shellPath(directory), MOCK_DSN: "fixture-connection-not-a-secret", MOCK_FILE_STATUS: "0", MOCK_UP_STATUS: "0", MOCK_VERIFY_STATUS: "0", ...overrides },
    });
    if (result.error) throw result.error;
    expect(result.stdout).not.toContain("fixture-connection-not-a-secret");
    expect(result.stderr).not.toContain("fixture-connection-not-a-secret");
    return result;
}

for (const service of ["migrate", "backend"]) {
    test(`${service} rejects an unreadable connection file without starting`, () => {
        const result = runCommand(service, { MOCK_FILE_STATUS: "23" });
        expect(result.status).toBe(23);
        expect(result.stdout).toBe("");
    });
    test(`${service} rejects an empty connection file without starting`, () => {
        const result = runCommand(service, { MOCK_DSN: "" });
        expect(result.status).toBe(1);
        expect(result.stdout).toBe("");
        expect(result.stderr).toContain("Database connection file is empty");
    });
}

test("migration applies and verifies the target schema in order", () => {
    const result = runCommand("migrate");
    expect(result.status).toBe(0);
    expect(result.stdout).toBe("migration:up\nmigration:verify\n");
});

test("migration failure is not masked by successful verification", () => {
    const result = runCommand("migrate", { MOCK_UP_STATUS: "24" });
    expect(result.status).toBe(24);
    expect(result.stdout).toBe("migration:up\n");
});

test("verification failure keeps the migration service unsuccessful", () => {
    const result = runCommand("migrate", { MOCK_VERIFY_STATUS: "25" });
    expect(result.status).toBe(25);
    expect(result.stdout).toBe("migration:up\nmigration:verify\n");
});

test("backend starts only after reading a nonempty connection file", () => {
    const result = runCommand("backend");
    expect(result.status).toBe(0);
    expect(result.stdout).toBe("backend-started\n");
});
