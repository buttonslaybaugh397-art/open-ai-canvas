import { spawnSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { parseArgs } from "node:util";
import { fileURLToPath } from "node:url";

const directory = path.dirname(fileURLToPath(import.meta.url));
const volumeKeys = [
  "backend-data",
  "deployment-secrets",
  "postgres-data",
  "redis-data",
];

// Only upgrades of an existing five-service deployment use this editor artifact.
export function createEditorCompose(resolved) {
  const compose = structuredClone(resolved);
  const { postgres, migrate, backend, web } = compose.services;
  const origins = backend.environment.CANVAS_CORS_ORIGINS;
  if (typeof origins !== "string" || !origins.trim())
    throw new Error(
      "CANVAS_CORS_ORIGINS must contain the actual browser origin.",
    );
  for (const origin of origins.split(",").map((value) => value.trim())) {
    let url;
    try {
      url = new URL(origin);
    } catch {
      throw new Error("CANVAS_CORS_ORIGINS contains an invalid origin.");
    }
    if (
      !["http:", "https:"].includes(url.protocol) ||
      url.username ||
      url.password ||
      origin !== url.origin ||
      url.hostname.includes("*")
    ) {
      throw new Error(
        "Use exact browser origins without credentials, paths, wildcards or a trailing slash.",
      );
    }
  }
  if (migrate.image !== backend.image)
    throw new Error("Migration and backend must use the same image.");
  for (const service of [migrate, backend, web]) {
    if (service.pull_policy !== "always")
      throw new Error("Application images must retain pull_policy: always.");
  }
  for (const key of volumeKeys) {
    const name = compose.volumes?.[key]?.name;
    if (typeof name !== "string" || !name.trim())
      throw new Error(
        `An explicit existing volume name is required for ${key}.`,
      );
    compose.volumes[key] = { name, external: true };
  }
  // No password is embedded, and a lost secrets volume must not generate a new one.
  postgres.environment.CANVAS_POSTGRES_PASSWORD = "";
  postgres.command[0] =
    '[ -s /run/canvas-secrets/postgres-password ] || { echo "Existing deployment password file is missing; restore the matching secrets volume" >&2; exit 1; }\n' +
    postgres.command[0];
  function checkInterpolation(value) {
    if (typeof value === "string" && /(?<!\$)\$(?:\{|[A-Za-z_])/.test(value))
      throw new Error(
        "Unresolved Compose variable or unescaped container shell variable in editor output.",
      );
    if (value && typeof value === "object")
      Object.values(value).forEach(checkInterpolation);
  }
  checkInterpolation(compose);
  return compose;
}

export function renderEditor({ envFile, output, imageTag }) {
  const env = { ...process.env };
  for (const key of Object.keys(env)) {
    if (/^(CANVAS_|POSTGRES_|COMPOSE_)/.test(key)) delete env[key];
  }
  env.POSTGRES_PASSWORD = "";
  if (imageTag) env.CANVAS_IMAGE_TAG = imageTag;
  const result = spawnSync(
    "docker",
    [
      "compose",
      "--env-file",
      path.resolve(envFile),
      "-f",
      path.join(directory, "docker-compose.yml"),
      "config",
      "--format",
      "json",
      "--no-normalize",
      "--no-path-resolution",
    ],
    {
      encoding: "utf8",
      env,
      maxBuffer: 2 * 1024 * 1024,
      timeout: 30000,
    },
  );
  if (result.error || result.status !== 0) {
    throw new Error(
      "Compose configuration failed. Check Docker Compose, the env-file path and required variables. Raw output is withheld to avoid disclosing private configuration.",
    );
  }
  const compose = createEditorCompose(JSON.parse(result.stdout));
  const content =
    "# PRIVATE: generated for an EXISTING 1Panel deployment; do not commit.\n# Verify browser origin, port and all four volume names against the server.\n# Stop the old web/backend before a schema upgrade; this file cannot stop them.\n" +
    Bun.YAML.stringify(compose, null, 2);
  const target = path.resolve(output);
  mkdirSync(path.dirname(target), { recursive: true });
  // Never overwrite a previously prepared deployment without explicit file handling.
  writeFileSync(target, content, { encoding: "utf8", flag: "wx", mode: 0o600 });
  console.log(`Editor compose written: ${target}`);
  console.log(
    "No database password embedded. Existing volumes and password file are required. No containers were started or updated.",
  );
}

if (import.meta.main) {
  try {
    const { values } = parseArgs({
      options: {
        "env-file": { type: "string" },
        output: { type: "string" },
        "image-tag": { type: "string" },
      },
    });
    if (!values["env-file"])
      throw new Error(
        "Usage: bun 1panel-deploy/render-editor.mjs --env-file <private.env> [--output <file>] [--image-tag <published-tag>]",
      );
    renderEditor({
      envFile: values["env-file"],
      output:
        values.output ||
        path.join(
          directory,
          "../.local/1panel-deploy/docker-compose.editor.yml",
        ),
      imageTag: values["image-tag"],
    });
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
