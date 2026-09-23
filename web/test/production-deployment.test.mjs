import { readFileSync } from "node:fs";

const read = (file) => readFileSync(new URL(`../../${file}`, import.meta.url), "utf8").replace(/\r\n/g, "\n");

test("production Compose keeps Web behind a loopback binding", () => {
    const compose = Bun.YAML.parse(read("docker-compose.deploy.yml"));
    const { postgres, redis, migrate, backend, web } = compose.services;

    expect(web.ports).toEqual(["${CANVAS_BIND_ADDRESS:-127.0.0.1}:${CANVAS_HTTP_PORT:-3000}:3000"]);
    for (const service of [postgres, redis, migrate, backend]) {
        expect(service.ports).toBeUndefined();
    }
});

test("Caddy sends all browser traffic through Web and keeps known streams unbuffered", () => {
    const caddyfile = read("Caddyfile.example");

    expect(caddyfile).toContain("reverse_proxy 127.0.0.1:3000");
    expect(caddyfile).toContain("flush_interval -1");
    expect(caddyfile).not.toContain("backend:8080");
});

test("GHCR installer downloads but does not overwrite the Caddy template", () => {
    const installer = read("scripts/install-server-image.sh");

    expect(installer).toContain("CADDYFILE_URL=");
    expect(installer).toContain('if [[ -f Caddyfile.example ]]; then');
    expect(installer).toContain("download_caddyfile");
});
