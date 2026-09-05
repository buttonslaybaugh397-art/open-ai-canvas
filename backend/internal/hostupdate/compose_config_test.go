package hostupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const inlineComposeFixture = `name: existing-canvas
services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: custom_user
      POSTGRES_DB: custom_database
      POSTGRES_PASSWORD: 'literal$$cash#word'
    command: [sh, -c, 'printf "%s" "$$POSTGRES_DB"']
    volumes:
      - postgres-data:/var/lib/postgresql/data
  redis:
    image: redis:7.4-alpine
    volumes:
      - redis-data:/data
  backend:
    image: ghcr.io/test-owner/open-ai-canvas-backend:1.2.8
    environment:
      CANVAS_DATABASE_DRIVER: postgres
      CANVAS_BACKEND_DATA_DIR: /data
      DATABASE_URL: 'host=postgres user=custom_user password=literal$$cash#word dbname=custom_database sslmode=disable'
      CANVAS_WORKER_CONCURRENCY: "37"
      CANVAS_CORS_ORIGINS: ""
      CANVAS_UPDATER_TOKEN: old-token-must-not-be-exported
      PRIVATE_TEST_VALUE: 'literal$$cash#word'
    volumes:
      - backend-data:/data
    labels:
      custom.label: keep
  web:
    image: ghcr.io/test-owner/open-ai-canvas-web:1.2.8
    ports:
      - "127.0.0.1:6987:3000"
volumes:
  postgres-data:
    name: original-postgres-data
  backend-data:
    name: original-backend-data
  redis-data:
    name: original-redis-data
`

func composeFixtureDocument(t *testing.T) composeDocument {
	t.Helper()
	var document composeDocument
	if err := json.Unmarshal([]byte(`{
 "name":"existing-canvas", "services": {
 "backend":{"image":"ghcr.io/test-owner/open-ai-canvas-backend:1.2.8","environment":{"CANVAS_DATABASE_DRIVER":"postgres","CANVAS_BACKEND_DATA_DIR":"/data","DATABASE_URL":"host=postgres user=custom_user dbname=custom_database sslmode=disable"},"volumes":[{"type":"volume","source":"backend-data","target":"/data"}]},
 "web":{"image":"ghcr.io/test-owner/open-ai-canvas-web:1.2.8","ports":[{"target":3000,"published":"6987","protocol":"tcp","host_ip":"127.0.0.1"}]},
 "postgres":{"image":"postgres:17-alpine","environment":{"POSTGRES_USER":"custom_user","POSTGRES_DB":"custom_database"},"volumes":[{"type":"volume","source":"postgres-data","target":"/var/lib/postgresql/data"}]},
 "redis":{"image":"redis:7.4-alpine","volumes":[{"type":"volume","source":"redis-data","target":"/data"}]}},"volumes":{"postgres-data":{"name":"original-postgres-data"}}}`), &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestDescribeInlineCompose(t *testing.T) {
	info, err := describeDeployment(composeFixtureDocument(t), "test-owner/open-ai-canvas")
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "1.2.8" || info.HealthURL != "http://127.0.0.1:6987/api/health/ready" || info.Values["POSTGRES_DB"] != "custom_database" || info.Values["COMPOSE_PROJECT_NAME"] != "existing-canvas" {
		t.Fatalf("wrong detected metadata: %#v", info)
	}
	for _, value := range info.Values {
		if strings.Contains(value, "password") {
			t.Fatal("metadata must not include database credentials")
		}
	}
}

func TestDescribeInlineComposeRejectsAmbiguity(t *testing.T) {
	cases := []struct {
		name   string
		change func(composeDocument)
	}{
		{"version mismatch", func(d composeDocument) {
			object(object(d["services"])["web"])["image"] = "ghcr.io/test-owner/open-ai-canvas-web:1.2.9"
		}},
		{"wrong owner", func(d composeDocument) {
			for _, s := range []string{"backend", "web"} {
				object(object(d["services"])[s])["image"] = "ghcr.io/other/open-ai-canvas-" + s + ":1.2.8"
			}
		}},
		{"dynamic port", func(d composeDocument) {
			object(object(d["services"])["web"])["ports"] = []any{map[string]any{"target": 3000, "published": "6000-7000", "protocol": "tcp"}}
		}},
		{"missing database", func(d composeDocument) {
			delete(object(object(object(d["services"])["postgres"])["environment"]), "POSTGRES_DB")
		}},
		{"missing redis", func(d composeDocument) { delete(object(d["services"]), "redis") }},
		{"missing project", func(d composeDocument) { delete(d, "name") }},
		{"unbacked data directory", func(d composeDocument) {
			object(object(object(d["services"])["backend"])["environment"])["CANVAS_BACKEND_DATA_DIR"] = "/other"
		}},
		{"external database", func(d composeDocument) {
			object(object(object(d["services"])["backend"])["environment"])["DATABASE_URL"] = "host=external user=custom_user dbname=custom_database"
		}},
		{"ephemeral data", func(d composeDocument) { delete(object(object(d["services"])["backend"]), "volumes") }},
		{"two ports", func(d composeDocument) {
			w := object(object(d["services"])["web"])
			w["ports"] = append(w["ports"].([]any), w["ports"].([]any)[0])
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := composeFixtureDocument(t)
			tc.change(d)
			if _, err := describeDeployment(d, "test-owner/open-ai-canvas"); err == nil {
				t.Fatal("unsafe/ambiguous deployment accepted")
			}
		})
	}
}

type secretFailureRunner struct{}

func (secretFailureRunner) Run(_ context.Context, _ string, _ []string, _ []string, _ io.Writer, stderr io.Writer) error {
	_, _ = io.WriteString(stderr, "PRIVATE_TOKEN=do-not-expose")
	return errors.New("PRIVATE_TOKEN=do-not-expose")
}

func TestComposeParseErrorDoesNotExposeSecrets(t *testing.T) {
	m := &Manager{config: Config{InstallDir: t.TempDir(), EnvFile: ".env"}, runner: secretFailureRunner{}}
	_, err := m.readCompose(context.Background(), "compose.yml", "")
	if err == nil || strings.Contains(err.Error(), "do-not-expose") {
		t.Fatal("raw config failure leaked")
	}
}

func TestConfiguratorDoesNotMutateOperationState(t *testing.T) {
	for _, phase := range []string{string(PhaseSwitching), string(PhaseManualIntervention), "migration"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			state := persistedState{Operation: Operation{Phase: Phase(phase)}}
			if phase == "migration" {
				state.Operation.Phase = PhaseIdle
				state.Migration.Phase = MigrationPhaseStopping
			}
			data, _ := json.Marshal(state)
			path := filepath.Join(dir, "state.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := NewConfigurator(Config{InstallDir: dir, StateDir: dir})
			if err == nil {
				t.Fatal("active/recovery state accepted")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(data, after) {
				t.Fatal("configuration rewrote operation state")
			}
			if _, err := os.Stat(filepath.Join(dir, "backups")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("read-only check created directories")
			}
		})
	}
}

func TestDeploymentMetadataAtomicValidation(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{config: Config{InstallDir: dir, EnvFile: ".env"}}
	original := []byte("# user settings\nPRIVATE_VALUE='literal$secret#text'\nCANVAS_IMAGE_TAG=1.2.8\n")
	if err := os.WriteFile(m.envPath(), original, 0o600); err != nil {
		t.Fatal(err)
	}
	err := m.writeDeploymentInfo(DeploymentInfo{Values: map[string]string{"CANVAS_IMAGE_TAG": "1.2.9", "ZZ_INVALID": "bad\nvalue"}})
	after, _ := os.ReadFile(m.envPath())
	if err == nil || !bytes.Equal(after, original) {
		t.Fatal("validation failure partially changed metadata")
	}
	if err := m.writeDeploymentInfo(DeploymentInfo{Values: map[string]string{"CANVAS_IMAGE_TAG": "1.2.9", "POSTGRES_USER": "custom"}}); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(m.envPath())
	if !bytes.Contains(after, []byte("PRIVATE_VALUE='literal$secret#text'")) || !bytes.Contains(after, []byte("CANVAS_IMAGE_TAG=1.2.9")) {
		t.Fatal("metadata merge lost unrelated settings")
	}
}

func TestPreserveInlineDeploymentDuringUpdate(t *testing.T) {
	local, target := composeFixtureDocument(t), composeFixtureDocument(t)
	ls, ts := object(local["services"]), object(target["services"])
	object(ls["backend"])["volumes"] = []any{map[string]any{"target": "/data", "source": "old-data", "type": "volume"}}
	object(ls["backend"])["environment"].(map[string]any)["CANVAS_WORKER_CONCURRENCY"] = "37"
	object(ts["backend"])["image"] = "ghcr.io/test-owner/open-ai-canvas-backend:1.2.9"
	object(ts["backend"])["healthcheck"] = map[string]any{"test": []any{"CMD", "wget"}}
	object(ts["backend"])["volumes"] = []any{map[string]any{"target": "/data", "source": "new-data"}, map[string]any{"target": "/run/open-ai-canvas-updater", "source": "/run/open-ai-canvas-updater"}}
	object(ts["web"])["ports"] = []any{map[string]any{"published": "3000", "target": 3000}}
	ls["sidecar"] = map[string]any{"image": "existing-sidecar:1"}
	preserveComposeDeployment(target, local)
	if target["name"] != local["name"] || !reflect.DeepEqual(ts["postgres"], ls["postgres"]) || !reflect.DeepEqual(object(ts["web"])["ports"], object(ls["web"])["ports"]) {
		t.Fatal("deployment infrastructure changed")
	}
	b := object(ts["backend"])
	if !strings.HasSuffix(text(b["image"]), ":1.2.9") || b["healthcheck"] == nil || object(b["environment"])["CANVAS_WORKER_CONCURRENCY"] != "37" {
		t.Fatal("target code or local settings lost")
	}
	mounts := b["volumes"].([]any)
	if len(mounts) != 2 || text(object(mounts[0])["source"]) != "old-data" || ts["sidecar"] == nil {
		t.Fatal("volumes duplicated or local service removed")
	}
}

func TestMigrationKeepsTargetInfrastructureAndSourceBusinessSettings(t *testing.T) {
	local, source := composeFixtureDocument(t), composeFixtureDocument(t)
	localBackend := object(object(local["services"])["backend"])
	sourceBackend := object(object(source["services"])["backend"])
	localBackend["environment"] = map[string]any{"DATABASE_URL": "target-db", "CANVAS_CORS_ORIGINS": "https://target.example", "CANVAS_WORKER_CONCURRENCY": "37", "CANVAS_DATABASE_DRIVER": "postgres"}
	sourceBackend["environment"] = map[string]any{"DATABASE_URL": "source-db", "CANVAS_CORS_ORIGINS": "https://source.example", "CANVAS_WORKER_CONCURRENCY": "19", "CANVAS_DATABASE_DRIVER": "postgres"}
	localBackend["command"] = []any{"target-database-transport"}
	delete(localBackend, "volumes")
	sourceBackend["command"] = []any{"source-database-transport"}
	sourceBackend["volumes"] = []any{map[string]any{"type": "bind", "source": "/source-only", "target": "/data"}}
	sourceBackend["image"] = "ghcr.io/test-owner/open-ai-canvas-backend:1.2.9"
	result := preserveMigrationDeployment(source, local)
	backend := object(object(result["services"])["backend"])
	env := object(backend["environment"])
	if env["DATABASE_URL"] != "target-db" || env["CANVAS_WORKER_CONCURRENCY"] != "19" || env["CANVAS_CORS_ORIGINS"] != "https://target.example" {
		t.Fatal("migration mixed source application settings with target infrastructure")
	}
	if backend["volumes"] != nil || backend["command"].([]any)[0] != "target-database-transport" || backend["image"] != sourceBackend["image"] {
		t.Fatal("migration imported source mounts or lost application version")
	}
}

// Opt-in uses Docker Compose's parser only. It never starts Docker containers.
func TestConfigureOnePanelComposeCLI(t *testing.T) {
	if os.Getenv("CANVAS_COMPOSE_CLI_TEST") != "1" {
		t.Skip("set CANVAS_COMPOSE_CLI_TEST=1 for real Compose parser verification")
	}
	template, err := os.ReadFile(filepath.Join("..", "..", "..", "docker-compose.1panel.yml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := strings.ReplaceAll(string(template), "buttonslaybaugh397-art", "test-owner")
	source = strings.ReplaceAll(source, ":-latest", ":-1.2.8")
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewConfigurator(Config{Repository: "test-owner/open-ai-canvas", InstallDir: dir, ComposeFile: "docker-compose.yml", ReleaseComposeFile: "docker-compose.1panel.yml", StateDir: filepath.Join(dir, "state")})
	if err != nil {
		t.Fatal(err)
	}
	before, err := m.readCompose(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ConfigureDeployment(context.Background(), "1.2.8", ""); err != nil {
		t.Fatal(err)
	}
	after, err := m.readCompose(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before["volumes"], after["volumes"]) || !reflect.DeepEqual(object(before["services"])["postgres"], object(after["services"])["postgres"]) {
		t.Fatal("1Panel automatic configuration changed secret/database volumes or initialization")
	}
}

func TestConfigureComposeCLIRoundTrip(t *testing.T) {
	if os.Getenv("CANVAS_COMPOSE_CLI_TEST") != "1" {
		t.Skip("set CANVAS_COMPOSE_CLI_TEST=1 for real Compose parser verification")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte(inlineComposeFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(Config{Repository: "test-owner/open-ai-canvas", InstallDir: dir, ComposeFile: "docker-compose.yml", StateDir: filepath.Join(dir, "state")})
	if err != nil {
		t.Fatal(err)
	}
	before, err := m.readCompose(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	info, err := m.ConfigureDeployment(context.Background(), "1.2.9", "")
	if err != nil {
		t.Fatal(err)
	}
	after, err := m.readCompose(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	bs, as := object(before["services"]), object(after["services"])
	if !reflect.DeepEqual(bs["postgres"], as["postgres"]) || !reflect.DeepEqual(before["volumes"], after["volumes"]) {
		t.Fatalf("configure changed PostgreSQL/dollar expansion or data volumes\nbefore=%#v\nafter=%#v", bs["postgres"], as["postgres"])
	}
	if !reflect.DeepEqual(object(bs["web"])["ports"], object(as["web"])["ports"]) {
		t.Fatal("port changed")
	}
	benv := object(object(as["backend"])["environment"])
	if benv["PRIVATE_TEST_VALUE"] != object(object(bs["backend"])["environment"])["PRIVATE_TEST_VALUE"] {
		t.Fatal("environment dollar expansion changed")
	}
	if _, ok := benv["CANVAS_UPDATER_TOKEN"]; ok {
		t.Fatal("bearer token exposed in backend environment")
	}
	if benv["CANVAS_UPDATER_TOKEN_FILE"] != "/run/open-ai-canvas-updater/token" || info.SocketDir != "/run/open-ai-canvas-updater" {
		t.Fatal("automatic socket wiring missing")
	}
	values, err := readEnvFile(m.envPath())
	if err != nil {
		t.Fatal(err)
	}
	if values["CANVAS_IMAGE_TAG"] != "1.2.9" || values["CANVAS_HTTP_PORT"] != "6987" || values["POSTGRES_USER"] != "custom_user" {
		t.Fatalf("metadata wrong: %#v", values)
	}
	envBytes, _ := os.ReadFile(m.envPath())
	if bytes.Contains(envBytes, []byte("literal")) || bytes.Contains(envBytes, []byte("old-token")) {
		t.Fatal("private settings copied to metadata")
	}
	backups, err := filepath.Glob(filepath.Join(m.config.BackupDir, "compose-before-configure-*.yml"))
	if err != nil || len(backups) != 1 {
		t.Fatal("original compose backup missing")
	}
	saved, _ := os.ReadFile(backups[0])
	if string(saved) != inlineComposeFixture {
		t.Fatal("original compose backup changed")
	}
	if _, err := m.ConfigureDeployment(context.Background(), "1.2.9", ""); err != nil {
		t.Fatal(err)
	}
	repeated, err := m.readCompose(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, repeated) {
		t.Fatal("repeat install changed normalized compose")
	}

	// A failed metadata write must put the user's original source back.
	m.config.EnvFile = "missing-directory/.env"
	sourceBefore, _ := os.ReadFile(path)
	if _, err := m.ConfigureDeployment(context.Background(), "1.2.10", ""); err == nil {
		t.Fatal("metadata write failure was ignored")
	}
	sourceAfter, _ := os.ReadFile(path)
	if !bytes.Equal(sourceBefore, sourceAfter) {
		t.Fatal("failed configure did not restore original compose")
	}
	m.config.EnvFile = ".env"

	// An update must take new images/healthchecks while retaining inline settings.
	m.config.ManagedCompose = true
	m.config.ReleaseComposeFile = "docker-compose.1panel.yml"
	targetSource := strings.ReplaceAll(inlineComposeFixture, "existing-canvas", "release-default")
	targetSource = strings.ReplaceAll(targetSource, "6987", "3000")
	targetSource = strings.ReplaceAll(targetSource, "custom_user", "release_user")
	targetSource = strings.ReplaceAll(targetSource, "original-", "release-")
	m.httpClient = &http.Client{Transport: composeRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(targetSource)), Header: make(http.Header)}, nil
	})}
	next, err := m.prepareTargetCompose("v1.2.10")
	if err != nil {
		t.Fatal(err)
	}
	target, err := m.readCompose(context.Background(), next, "")
	if err != nil {
		t.Fatal(err)
	}
	ts := object(target["services"])
	if target["name"] != after["name"] || !reflect.DeepEqual(ts["postgres"], as["postgres"]) || !reflect.DeepEqual(target["volumes"], after["volumes"]) || !reflect.DeepEqual(object(ts["web"])["ports"], object(as["web"])["ports"]) {
		t.Fatal("target compose overwrote deployment infrastructure")
	}
	if text(object(ts["backend"])["image"]) != "ghcr.io/test-owner/open-ai-canvas-backend:1.2.10" {
		t.Fatal("target version did not override literal image tag")
	}

	// The standard Release requires credentials even when all current settings are
	// inline. Resolve them only in the child environment, including literal dollars.
	standardTemplate, err := os.ReadFile(filepath.Join("..", "..", "..", "docker-compose.deploy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	m.config.ReleaseComposeFile = "docker-compose.deploy.yml"
	targetSource = string(standardTemplate)
	next, err = m.prepareTargetCompose("v1.2.10")
	if err != nil {
		t.Fatal(err)
	}
	target, err = m.readCompose(context.Background(), next, "")
	if err != nil {
		t.Fatal(err)
	}
	ts = object(target["services"])
	connection := object(object(as["backend"])["environment"])["DATABASE_URL"]
	if object(object(ts["migrate"])["environment"])["DATABASE_URL"] != connection || object(object(ts["backend"])["environment"])["DATABASE_URL"] != connection {
		t.Fatal("migration/backend credentials changed during template resolution")
	}
	if !reflect.DeepEqual(ts["postgres"], as["postgres"]) || target["name"] != "existing-canvas" {
		t.Fatal("standard template changed the database or project")
	}
	envBytes, _ = os.ReadFile(m.envPath())
	if bytes.Contains(envBytes, []byte("literal")) {
		t.Fatal("template credentials leaked into metadata")
	}
	sourceAfter, _ = os.ReadFile(path)
	if !bytes.Equal(sourceBefore, sourceAfter) {
		t.Fatal("preparing target modified running deployment")
	}

	// A later edit in 1Panel, not stale .env metadata, determines the next check.
	object(object(after["services"])["web"])["ports"] = []any{map[string]any{"target": 3000, "published": "7001", "protocol": "tcp", "host_ip": "127.0.0.1"}}
	if err := m.writeCompose(context.Background(), after, path, false); err != nil {
		t.Fatal(err)
	}
	if err := m.syncComposeConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	values, err = readEnvFile(m.envPath())
	if err != nil {
		t.Fatal(err)
	}
	if values["CANVAS_HTTP_PORT"] != "7001" || m.healthURL() != "http://127.0.0.1:7001/api/health/ready" {
		t.Fatal("inline port edit was not detected")
	}
}
