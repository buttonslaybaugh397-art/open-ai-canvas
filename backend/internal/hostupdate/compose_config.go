package hostupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type composeDocument map[string]any

type DeploymentInfo struct {
	Version   string
	SocketDir string
	HealthURL string
	Values    map[string]string
}

var deploymentImage = regexp.MustCompile(`^ghcr\.io/([a-zA-Z0-9_.-]+)/open-ai-canvas-(backend|web):([a-zA-Z0-9_][a-zA-Z0-9_.-]*)$`)
var deploymentName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
var deploymentVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[.-][a-zA-Z0-9_.-]+)?$`)
var socketDirectory = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

func object(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func text(value any) string {
	result, _ := value.(string)
	return result
}

func pinComposeImages(document composeDocument, version string) error {
	if !deploymentVersion.MatchString(version) {
		return errors.New("必须指定有效的 Release 版本")
	}
	services := object(document["services"])
	for _, name := range []string{"backend", "web", "migrate"} {
		service := object(services[name])
		if service == nil {
			continue
		}
		match := deploymentImage.FindStringSubmatch(text(service["image"]))
		if match == nil {
			return fmt.Errorf("无法识别 %s 的 GHCR 影策镜像", name)
		}
		service["image"] = strings.TrimSuffix(text(service["image"]), match[3]) + strings.TrimPrefix(version, "v")
	}
	return nil
}

func (m *Manager) composeArguments(path string) []string {
	args := []string{"compose", "--project-directory", m.config.InstallDir}
	if _, err := os.Stat(m.envPath()); err == nil || !errors.Is(err, os.ErrNotExist) {
		args = append(args, "--env-file", m.envPath())
	}
	return append(args, "-f", path)
}

// Docker resolves anchors, env_file, interpolation, relative mounts and project
// volume names. Never parse YAML with shell expressions or print resolved secrets.
func (m *Manager) readCompose(ctx context.Context, path, version string, environment ...string) (composeDocument, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := append(m.composeArguments(path), "config", "--format", "json")
	var output, stderr bytes.Buffer
	if version != "" {
		environment = append(environment, "CANVAS_IMAGE_TAG="+strings.TrimPrefix(version, "v"))
	}
	if err := m.runner.Run(ctx, "docker", args, environment, &output, &stderr); err != nil {
		return nil, errors.New("无法解析部署编排，请在服务器检查 docker compose config；已隐藏可能包含密钥的输出")
	}
	if output.Len() > 8<<20 {
		return nil, errors.New("部署编排超过 8 MiB")
	}
	var document composeDocument
	if err := json.Unmarshal(output.Bytes(), &document); err != nil || object(document["services"]) == nil {
		return nil, errors.New("Docker 未返回有效的 Compose JSON 配置")
	}
	return document, nil
}

func describeDeployment(document composeDocument, repository string) (DeploymentInfo, error) {
	services := object(document["services"])
	backend, web, postgres := object(services["backend"]), object(services["web"]), object(services["postgres"])
	if backend == nil || web == nil || postgres == nil || object(services["redis"]) == nil {
		return DeploymentInfo{}, errors.New("自动配置要求编排包含 backend、web、postgres 和 redis 服务")
	}
	owner, version := "", ""
	for _, name := range []string{"backend", "web", "migrate"} {
		service := object(services[name])
		if service == nil {
			continue
		}
		match := deploymentImage.FindStringSubmatch(text(service["image"]))
		expected := name
		if name == "migrate" {
			expected = "backend"
		}
		if match == nil || match[2] != expected {
			return DeploymentInfo{}, fmt.Errorf("%s 必须使用带固定版本标签的 GHCR 影策镜像", name)
		}
		if owner != "" && (owner != match[1] || version != match[3]) {
			return DeploymentInfo{}, errors.New("backend、web 与 migrate 的镜像仓库或版本不一致")
		}
		owner, version = match[1], match[3]
	}
	if !deploymentVersion.MatchString(version) {
		return DeploymentInfo{}, errors.New("镜像版本必须固定到 Release，不能使用 latest 或开发标签")
	}
	if strings.SplitN(repository, "/", 2)[0] != owner {
		return DeploymentInfo{}, errors.New("更新器仓库与编排镜像的所有者不一致")
	}
	env := object(backend["environment"])
	driver := strings.ToLower(text(env["CANVAS_DATABASE_DRIVER"]))
	if driver != "postgres" && driver != "postgresql" {
		return DeploymentInfo{}, errors.New("自动安装 Host Updater 仅支持 PostgreSQL 部署")
	}
	pgEnv := object(postgres["environment"])
	user, database := text(pgEnv["POSTGRES_USER"]), text(pgEnv["POSTGRES_DB"])
	if !deploymentName.MatchString(user) || !deploymentName.MatchString(database) {
		return DeploymentInfo{}, errors.New("编排必须明确指定 PostgreSQL 用户与数据库名，且只包含字母、数字和下划线")
	}
	if text(env["CANVAS_BACKEND_DATA_DIR"]) != "/data" {
		return DeploymentInfo{}, errors.New("自动配置要求 CANVAS_BACKEND_DATA_DIR=/data，以匹配完整备份目录")
	}
	for name, path := range map[string]string{"backend": "/data", "redis": "/data", "postgres": "/var/lib/postgresql/data"} {
		if _, ok := persistentMount(object(services[name]), path); !ok {
			return DeploymentInfo{}, fmt.Errorf("%s 的 %s 必须使用明确的持久卷或 bind 挂载", name, path)
		}
	}
	if connection := text(env["DATABASE_URL"]); connection != "" {
		parsed, err := pgconn.ParseConfig(connection)
		if err != nil || parsed.Host != "postgres" || parsed.Port != 5432 || parsed.User != user || parsed.Database != database {
			return DeploymentInfo{}, errors.New("DATABASE_URL 必须指向编排内 postgres 服务及相同数据库/用户，避免备份错误数据库")
		}
	} else {
		backendSecrets, backendOK := persistentMount(backend, "/run/canvas-secrets")
		postgresSecrets, postgresOK := persistentMount(postgres, "/run/canvas-secrets")
		command, _ := json.Marshal(backend["command"])
		if !backendOK || !postgresOK || backendSecrets["source"] != postgresSecrets["source"] || backendSecrets["type"] != postgresSecrets["type"] || !strings.Contains(string(command), "cat /run/canvas-secrets/database-url") {
			return DeploymentInfo{}, errors.New("无法确认 backend 与 postgres 使用同一数据库连接；自动配置仅支持内联 DATABASE_URL 或标准共享密钥卷")
		}
	}
	project := text(document["name"])
	if project == "" {
		return DeploymentInfo{}, errors.New("无法识别 Compose 项目名，拒绝创建可能指向新数据卷的部署")
	}
	ports, _ := web["ports"].([]any)
	var address, port string
	for _, item := range ports {
		mapping := object(item)
		if fmt.Sprint(mapping["target"]) != "3000" || text(mapping["protocol"]) != "tcp" {
			continue
		}
		candidate := fmt.Sprint(mapping["published"])
		if port != "" {
			return DeploymentInfo{}, errors.New("web 存在多个 3000 端口映射，请保留一个明确的 HTTP 入口")
		}
		number, err := strconv.Atoi(candidate)
		if err != nil || number < 1 || number > 65535 {
			return DeploymentInfo{}, errors.New("web 必须发布固定的 HTTP 端口，不能使用随机端口或范围")
		}
		port, address = candidate, text(mapping["host_ip"])
	}
	if port == "" {
		return DeploymentInfo{}, errors.New("无法从编排识别 web 的 HTTP 端口映射")
	}
	if address == "" || address == "0.0.0.0" {
		address = "127.0.0.1"
	} else if address == "::" {
		address = "::1"
	}
	if net.ParseIP(address) == nil {
		return DeploymentInfo{}, errors.New("web 的宿主机绑定地址不是有效 IP")
	}
	info := DeploymentInfo{Version: strings.TrimPrefix(version, "v"), HealthURL: "http://" + net.JoinHostPort(address, port) + "/api/health/ready", Values: map[string]string{
		"CANVAS_IMAGE_TAG": strings.TrimPrefix(version, "v"), "CANVAS_IMAGE_OWNER": owner,
		"CANVAS_DATABASE_DRIVER": "postgres", "POSTGRES_USER": user, "POSTGRES_DB": database,
		"COMPOSE_PROJECT_NAME": project, "CANVAS_HTTP_PORT": port,
	}}
	info.Values["CANVAS_UPDATER_HEALTH_URL"] = info.HealthURL
	volumes, _ := backend["volumes"].([]any)
	for _, item := range volumes {
		mount := object(item)
		if text(mount["target"]) != "/run/open-ai-canvas-updater" {
			continue
		}
		if text(mount["type"]) != "bind" || info.SocketDir != "" {
			return DeploymentInfo{}, errors.New("更新器 Socket 目录必须是唯一的宿主机 bind 挂载")
		}
		info.SocketDir = text(mount["source"])
	}
	return info, nil
}

func persistentMount(service map[string]any, target string) (map[string]any, bool) {
	mounts, _ := service["volumes"].([]any)
	var result map[string]any
	for _, item := range mounts {
		mount := object(item)
		if text(mount["target"]) != target {
			continue
		}
		if result != nil || text(mount["source"]) == "" || (text(mount["type"]) != "volume" && text(mount["type"]) != "bind") {
			return nil, false
		}
		result = mount
	}
	return result, result != nil
}

// The generated .env is metadata for the existing backup/restore contract, not
// the source of truth for inline deployment values. Keep unrelated user entries.
func (m *Manager) writeDeploymentInfo(info DeploymentInfo) error {
	data, err := os.ReadFile(m.envPath())
	if errors.Is(err, os.ErrNotExist) {
		data = []byte("# Host Updater metadata; maintain runtime settings in Compose.\n")
	} else if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	keys := make([]string, 0, len(info.Values))
	for key := range info.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := info.Values[key]
		if strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("部署配置包含无效控制字符")
		}
		found := false
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), key+"=") {
				lines[i] = key + "=" + value
				found = true
			}
		}
		if !found {
			lines = append(lines, key+"="+value)
		}
	}
	result := []byte(strings.Join(lines, "\n") + "\n")
	if bytes.Equal(data, result) {
		return nil
	}
	return writeMigrationFile(m.envPath(), result)
}

func (m *Manager) syncComposeConfig(ctx context.Context) error {
	if !m.config.ManagedCompose {
		return nil
	}
	document, err := m.readCompose(ctx, m.composePath(), "")
	if err != nil {
		return err
	}
	info, err := describeDeployment(document, m.config.Repository)
	if err != nil {
		return err
	}
	if err := m.writeDeploymentInfo(info); err != nil {
		return err
	}
	return nil
}

func (m *Manager) ConfigureDeployment(ctx context.Context, version, requestedSocketDir string) (_ DeploymentInfo, resultErr error) {
	if !deploymentVersion.MatchString(version) {
		return DeploymentInfo{}, errors.New("必须指定有效的 Release 版本")
	}
	if err := os.MkdirAll(m.config.BackupDir, 0o700); err != nil {
		return DeploymentInfo{}, err
	}
	originalCompose, err := os.ReadFile(m.composePath())
	if err != nil {
		return DeploymentInfo{}, err
	}
	originalEnv, envErr := os.ReadFile(m.envPath())
	if envErr != nil && !errors.Is(envErr, os.ErrNotExist) {
		return DeploymentInfo{}, envErr
	}
	changed := false
	defer func() {
		if !changed || resultErr == nil {
			return
		}
		var restoreErr error
		if err := writeMigrationFile(m.composePath(), originalCompose); err != nil {
			restoreErr = errors.Join(restoreErr, err)
		}
		if errors.Is(envErr, os.ErrNotExist) {
			if err := os.Remove(m.envPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
				restoreErr = errors.Join(restoreErr, err)
			}
		} else if err := writeMigrationFile(m.envPath(), originalEnv); err != nil {
			restoreErr = errors.Join(restoreErr, err)
		}
		if restoreErr != nil {
			resultErr = errors.Join(resultErr, errors.New("自动配置失败且原配置未能完全恢复，请检查配置备份；未重建业务服务"))
		}
	}()
	document, err := m.readCompose(ctx, m.composePath(), "")
	if err != nil {
		return DeploymentInfo{}, err
	}
	if err := pinComposeImages(document, version); err != nil {
		return DeploymentInfo{}, err
	}
	services := object(document["services"])
	info, err := describeDeployment(document, m.config.Repository)
	if err != nil {
		return DeploymentInfo{}, err
	}
	if requestedSocketDir != "" && info.SocketDir != "" && requestedSocketDir != info.SocketDir {
		return DeploymentInfo{}, errors.New("指定的 Socket 目录与现有挂载不同，拒绝静默切换")
	}
	info.SocketDir = firstNonEmpty(requestedSocketDir, info.SocketDir, "/run/open-ai-canvas-updater")
	if !socketDirectory.MatchString(info.SocketDir) || strings.Contains(info.SocketDir, "..") || info.SocketDir == "/" {
		return DeploymentInfo{}, errors.New("Socket 目录必须是无空格的独立绝对路径")
	}
	backend := object(services["backend"])
	env := object(backend["environment"])
	env["CANVAS_UPDATER_SOCKET"] = "/run/open-ai-canvas-updater/updater.sock"
	env["CANVAS_UPDATER_TOKEN_FILE"] = "/run/open-ai-canvas-updater/token"
	delete(env, "CANVAS_UPDATER_TOKEN")
	delete(env, "CANVAS_UPDATER_URL")
	volumes, _ := backend["volumes"].([]any)
	found := false
	for _, item := range volumes {
		if mount := object(item); text(mount["target"]) == "/run/open-ai-canvas-updater" {
			mount["read_only"] = true
			found = true
		}
	}
	if !found {
		backend["volumes"] = append(volumes, map[string]any{"type": "bind", "source": info.SocketDir, "target": "/run/open-ai-canvas-updater", "read_only": true})
	}
	if err := m.writeCompose(ctx, document, m.composePath(), true); err != nil {
		return DeploymentInfo{}, err
	}
	changed = true
	info.Values["CANVAS_UPDATER_CONFIG_SOURCE"] = "compose"
	info.Values["CANVAS_UPDATER_REPOSITORY"] = m.config.Repository
	info.Values["CANVAS_UPDATER_COMPOSE_FILE"] = m.config.ComposeFile
	info.Values["CANVAS_UPDATER_RELEASE_COMPOSE_FILE"] = m.config.ReleaseComposeFile
	info.Values["CANVAS_UPDATER_SOCKET_DIR"] = info.SocketDir
	if err := m.writeDeploymentInfo(info); err != nil {
		return DeploymentInfo{}, err
	}
	return info, nil
}

func (m *Manager) writeCompose(ctx context.Context, document composeDocument, destination string, backup bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return errors.New("无法序列化部署编排")
	}
	stage, err := os.CreateTemp(m.config.InstallDir, ".canvas-compose-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(stage.Name())
	if _, err := stage.Write(data); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	// config emits interpolation-safe YAML (including $$ in shell commands).
	var output, stderr bytes.Buffer
	args := append(m.composeArguments(stage.Name()), "config", "--format", "yaml")
	if err := m.runner.Run(ctx, "docker", args, nil, &output, &stderr); err != nil {
		return errors.New("生成的编排未通过 Docker 校验；未替换原编排")
	}
	if backup {
		backupPath := filepath.Join(m.config.BackupDir, "compose-before-configure-"+randomID()+".yml")
		if err := replaceFile(destination, backupPath, 0o600); err != nil {
			return fmt.Errorf("备份原编排：%w", err)
		}
	}
	if err := os.WriteFile(stage.Name(), output.Bytes(), 0o600); err != nil {
		return err
	}
	return replaceFile(stage.Name(), destination, 0o600)
}

func preserveComposeDeployment(target, local composeDocument) {
	for _, key := range []string{"name", "volumes", "networks", "secrets", "configs"} {
		if value, exists := local[key]; exists {
			if left, right := object(target[key]), object(value); left != nil && right != nil {
				for name, item := range right {
					left[name] = item
				}
			} else {
				target[key] = value
			}
		}
	}
	services, localServices := object(target["services"]), object(local["services"])
	for name, value := range localServices {
		current, next := object(value), object(services[name])
		if name != "backend" && name != "web" && name != "migrate" || next == nil {
			services[name] = value
			continue
		}
		for key, item := range current {
			switch key {
			case "image", "build", "pull_policy", "healthcheck", "depends_on", "command", "entrypoint":
				continue
			case "environment":
				env := object(next[key])
				if env == nil {
					env = map[string]any{}
					next[key] = env
				}
				for k, v := range object(item) {
					if k == "CANVAS_AUTO_MIGRATE" && services["migrate"] != nil {
						continue
					}
					env[k] = v
				}
			case "volumes":
				mounts, _ := item.([]any)
				targets := map[string]bool{}
				for _, mount := range mounts {
					targets[text(object(mount)["target"])] = true
				}
				additions, _ := next[key].([]any)
				for _, mount := range additions {
					if !targets[text(object(mount)["target"])] {
						mounts = append(mounts, mount)
					}
				}
				next[key] = mounts
			default:
				next[key] = item
			}
		}
	}
	// A newly introduced migration service must use the same database as backend.
	if migrate := object(services["migrate"]); migrate != nil {
		env := object(migrate["environment"])
		if env == nil {
			env = map[string]any{}
			migrate["environment"] = env
		}
		backend := object(services["backend"])
		for _, key := range []string{"CANVAS_DATABASE_DRIVER", "DATABASE_URL"} {
			if value, exists := object(backend["environment"])[key]; exists {
				env[key] = value
			}
		}
		if networks, exists := backend["networks"]; exists {
			migrate["networks"] = networks
		}
	}
}

// Template interpolation runs in a child process only. Inline credentials must
// not be copied into metadata, command arguments, logs, or the parent environment.
func composeTemplateEnvironment(local composeDocument, repository string) ([]string, error) {
	info, err := describeDeployment(local, repository)
	if err != nil {
		return nil, err
	}
	values := info.Values
	services := object(local["services"])
	for service, keys := range map[string][]string{
		"postgres": {"POSTGRES_PASSWORD"},
		"backend":  {"DATABASE_URL"},
	} {
		for _, key := range keys {
			values[key] = text(object(object(services[service])["environment"])[key])
		}
	}
	var environment []string
	for key, value := range values {
		environment = append(environment, key+"="+value)
	}
	return environment, nil
}

// Import application settings and images, never source-machine mounts, network
// identities or database credentials. Database transport remains target-local.
func preserveMigrationDeployment(source, local composeDocument) composeDocument {
	services, saved := object(local["services"]), object(source["services"])
	infrastructureKey := func(key string) bool {
		switch key {
		case "DATABASE_URL", "REDIS_URL", "CANVAS_DATABASE_DRIVER", "CANVAS_BACKEND_DATA_DIR", "CANVAS_BACKEND_ADDR", "CANVAS_AUTO_MIGRATE", "CANVAS_CORS_ORIGINS":
			return true
		}
		return strings.HasPrefix(key, "CANVAS_UPDATER_")
	}
	for _, name := range []string{"backend", "web", "migrate"} {
		current, imported := object(services[name]), object(saved[name])
		if current == nil {
			continue
		}
		if imported == nil {
			if name == "migrate" {
				current["image"] = object(saved["backend"])["image"]
			}
			continue
		}
		current["image"] = imported["image"]
		if value, exists := imported["healthcheck"]; exists {
			current["healthcheck"] = value
		}
		env := map[string]any{}
		for key, value := range object(imported["environment"]) {
			if !infrastructureKey(key) {
				env[key] = value
			}
		}
		for key, value := range object(current["environment"]) {
			if infrastructureKey(key) {
				env[key] = value
			}
		}
		if len(env) > 0 {
			current["environment"] = env
		} else {
			delete(current, "environment")
		}
	}
	return local
}
