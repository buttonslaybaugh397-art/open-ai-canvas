package hostupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
}

func (m *Manager) latestRelease(ctx context.Context) (*Release, error) {
	url := "https://api.github.com/repos/" + m.config.Repository + "/releases?per_page=30"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "open-ai-canvas-host-updater")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if m.config.GitHubToken != "" {
		request.Header.Set("Authorization", "Bearer "+m.config.GitHubToken)
	}
	response, err := m.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub Release：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil, fmt.Errorf("GitHub Release 返回 HTTP %d", response.StatusCode)
	}
	var releases []githubRelease
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxReleaseResponseBytes))
	if err := decoder.Decode(&releases); err != nil {
		return nil, fmt.Errorf("解析 GitHub Release：%w", err)
	}
	filtered := make([]githubRelease, 0, len(releases))
	for _, release := range releases {
		if !release.Draft && strings.HasPrefix(release.TagName, "v") {
			filtered = append(filtered, release)
		}
	}
	if len(filtered) == 0 {
		return nil, errors.New("GitHub 尚未发布可用 Release")
	}
	sort.SliceStable(filtered, func(i, j int) bool { return CompareVersions(filtered[i].TagName, filtered[j].TagName) > 0 })
	latest := filtered[0]
	return &Release{Version: latest.TagName, Name: latest.Name, Body: latest.Body, URL: latest.HTMLURL, PublishedAt: latest.PublishedAt, Prerelease: latest.Prerelease}, nil
}

func (m *Manager) currentVersion() (string, error) {
	values, err := readEnvFile(m.envPath())
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(values["CANVAS_IMAGE_TAG"])
	if version == "" || version == "latest" {
		return "", errors.New("CANVAS_IMAGE_TAG 必须固定为发布版本，不能使用 latest")
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version, nil
}

func (m *Manager) prepareTargetCompose(targetVersion string) (string, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", m.config.Repository, targetVersion, m.config.ReleaseComposeFile)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	response, err := m.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("下载目标 Compose：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载目标 Compose 返回 HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", errors.New("目标 Compose 文件为空")
	}
	path := filepath.Join(m.config.StateDir, "compose-"+strings.TrimPrefix(targetVersion, "v")+".next.yml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("保存目标 Compose：%w", err)
	}
	return path, nil
}

func (m *Manager) preflight(composePath, targetVersion string) error {
	if _, err := os.Stat(m.composePath()); err != nil {
		return fmt.Errorf("读取当前 Compose：%w", err)
	}
	if _, err := os.Stat(m.envPath()); err != nil {
		return fmt.Errorf("读取部署环境：%w", err)
	}
	if m.config.SelfUpdate {
		if strings.TrimSpace(m.config.BinaryPath) == "" {
			return errors.New("检查 Host Updater 安装目录：二进制路径为空")
		}
		if err := checkWritableDirectory(filepath.Dir(m.config.BinaryPath)); err != nil {
			return fmt.Errorf("检查 Host Updater 安装目录：%w", err)
		}
	}
	if err := checkBackupDiskSpace(m.config.BackupDir); err != nil {
		return err
	}
	if err := m.compose(composePath, targetVersion, 2*time.Minute, nil, "config", "--quiet"); err != nil {
		return fmt.Errorf("目标 Compose 校验失败：%w", err)
	}
	current, err := m.currentVersion()
	if err != nil {
		return err
	}
	if err := m.checkHealthOnce(m.healthURL(), current); err != nil {
		return fmt.Errorf("当前运行版本与部署配置不一致或服务未就绪：%w", err)
	}
	return nil
}

func checkWritableDirectory(directory string) error {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return errors.New("Host Updater 二进制路径为空")
	}
	temporary, err := os.CreateTemp(directory, ".updater-write-test-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	if closeErr := temporary.Close(); closeErr != nil {
		_ = os.Remove(name)
		return closeErr
	}
	if removeErr := os.Remove(name); removeErr != nil {
		return removeErr
	}
	return nil
}

func (m *Manager) prepareUpdaterBinary(targetVersion string) (string, error) {
	if !m.config.SelfUpdate {
		return "", nil
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return "", fmt.Errorf("Host Updater 自更新不支持 %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if strings.TrimSpace(m.config.BinaryPath) == "" {
		return "", errors.New("未配置 Host Updater 二进制路径")
	}
	baseURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/", m.config.Repository, targetVersion)
	asset := "open-ai-canvas-host-updater-linux-" + runtime.GOARCH
	checksums, err := m.downloadReleaseAsset(baseURL+"SHA256SUMS", 1<<20)
	if err != nil {
		return "", fmt.Errorf("下载 Host Updater 校验清单：%w", err)
	}
	expected := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			expected = fields[0]
			break
		}
	}
	if len(expected) != 64 {
		return "", fmt.Errorf("目标 Release 的 SHA256SUMS 缺少 %s", asset)
	}
	binary, err := m.downloadReleaseAsset(baseURL+asset, 128<<20)
	if err != nil {
		return "", fmt.Errorf("下载目标 Host Updater：%w", err)
	}
	hash := sha256.Sum256(binary)
	actual := hex.EncodeToString(hash[:])
	if actual != expected {
		return "", fmt.Errorf("Host Updater 校验失败：期望 %s，实际 %s", expected, actual)
	}
	path := filepath.Join(m.config.StateDir, asset+"-"+strings.TrimPrefix(targetVersion, "v")+".next")
	if err := os.WriteFile(path, binary, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func (m *Manager) downloadReleaseAsset(url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "open-ai-canvas-host-updater")
	response, err := m.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	reader := io.LimitReader(response.Body, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("Release 资产超过允许大小")
	}
	return data, nil
}

func (m *Manager) restartSelf() {
	time.Sleep(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = m.runner.Run(ctx, "systemctl", []string{"restart", m.config.ServiceName}, nil, io.Discard, io.Discard)
}

func (m *Manager) compose(composePath, imageTag string, timeout time.Duration, stdout io.Writer, arguments ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := []string{"compose", "--env-file", m.envPath(), "-f", composePath}
	args = append(args, arguments...)
	var stderr bytes.Buffer
	if stdout == nil {
		stdout = io.Discard
	}
	err := m.runner.Run(ctx, "docker", args, []string{"CANVAS_IMAGE_TAG=" + strings.TrimPrefix(imageTag, "v")}, stdout, &stderr)
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if len(message) > 1000 {
			message = message[len(message)-1000:]
		}
		if message != "" {
			return fmt.Errorf("docker compose %s：%s", strings.Join(arguments, " "), message)
		}
		return fmt.Errorf("docker compose %s：%w", strings.Join(arguments, " "), err)
	}
	return nil
}

func (m *Manager) verifyImages(targetVersion string) error {
	owner := strings.SplitN(m.config.Repository, "/", 2)[0]
	for _, image := range []string{"ghcr.io/" + owner + "/open-ai-canvas-backend:", "ghcr.io/" + owner + "/open-ai-canvas-web:"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		var output bytes.Buffer
		var stderr bytes.Buffer
		err := m.runner.Run(ctx, "docker", []string{"image", "inspect", image + strings.TrimPrefix(targetVersion, "v"), "--format", "{{json .RepoDigests}}"}, nil, &output, &stderr)
		cancel()
		if err != nil {
			return fmt.Errorf("校验目标镜像摘要失败：%s", strings.TrimSpace(stderr.String()))
		}
		if !strings.Contains(output.String(), "@sha256:") {
			return fmt.Errorf("目标镜像 %s 未包含仓库摘要", image+targetVersion)
		}
	}
	return nil
}

func (m *Manager) createBackup(version string) (Backup, error) {
	now := time.Now().UTC()
	id := "backup-" + now.Format("20060102-150405") + "-" + randomID()[:8]
	path := filepath.Join(m.config.BackupDir, id+".zip")
	temporary := path + ".partial"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Backup{}, fmt.Errorf("创建备份文件：%w", err)
	}
	removeTemporary := true
	defer func() {
		_ = file.Close()
		if removeTemporary {
			_ = os.Remove(temporary)
		}
	}()
	hasher := sha256.New()
	archive := zip.NewWriter(io.MultiWriter(file, hasher))
	metadata, _ := json.MarshalIndent(map[string]any{"id": id, "version": version, "createdAt": now, "format": 2}, "", "  ")
	if err := writeZipBytes(archive, "metadata.json", metadata); err != nil {
		return Backup{}, err
	}
	if err := writeZipBytesFromFile(archive, "service/.env", m.envPath()); err != nil {
		return Backup{}, fmt.Errorf("备份部署环境：%w", err)
	}
	if err := writeZipBytesFromFile(archive, "service/compose.yml", m.composePath()); err != nil {
		return Backup{}, fmt.Errorf("备份 Compose 配置：%w", err)
	}
	databaseEntry, err := archive.CreateHeader(&zip.FileHeader{Name: "database.dump", Method: zip.Store})
	if err != nil {
		return Backup{}, err
	}
	values, err := readEnvFile(m.envPath())
	if err != nil {
		return Backup{}, err
	}
	postgresUser := firstNonEmpty(values["POSTGRES_USER"], "open_ai_canvas")
	postgresDB := firstNonEmpty(values["POSTGRES_DB"], "open_ai_canvas")
	if err := m.compose(m.composePath(), version, m.config.StepTimeout, databaseEntry, "exec", "-T", "postgres", "pg_dump", "-U", postgresUser, "-d", postgresDB, "-Fc"); err != nil {
		return Backup{}, fmt.Errorf("备份 PostgreSQL：%w", err)
	}
	dataEntry, err := archive.CreateHeader(&zip.FileHeader{Name: "backend-data.tar", Method: zip.Store})
	if err != nil {
		return Backup{}, err
	}
	if err := m.compose(m.composePath(), version, m.config.StepTimeout, dataEntry, "run", "--rm", "--no-deps", "--entrypoint", "sh", "--user", "root", "backend", "-c", "tar -C /data -cf - ."); err != nil {
		return Backup{}, fmt.Errorf("备份数据目录：%w", err)
	}
	if err := m.compose(m.composePath(), version, 2*time.Minute, nil, "exec", "-T", "redis", "redis-cli", "SAVE"); err != nil {
		return Backup{}, fmt.Errorf("固化 Redis 数据：%w", err)
	}
	if err := m.compose(m.composePath(), version, 2*time.Minute, nil, "stop", "redis"); err != nil {
		return Backup{}, fmt.Errorf("停止 Redis：%w", err)
	}
	redisEntry, err := archive.CreateHeader(&zip.FileHeader{Name: "redis-data.tar", Method: zip.Store})
	if err != nil {
		return Backup{}, err
	}
	if err := m.compose(m.composePath(), version, m.config.StepTimeout, redisEntry, "run", "--rm", "--no-deps", "--entrypoint", "sh", "--user", "root", "redis", "-c", "tar -C /data -cf - ."); err != nil {
		return Backup{}, fmt.Errorf("备份 Redis 数据目录：%w", err)
	}
	secretsEntry, err := archive.CreateHeader(&zip.FileHeader{Name: "deployment-secrets.tar", Method: zip.Store})
	if err != nil {
		return Backup{}, err
	}
	secretsCommand := "if grep -qs ' /run/canvas-secrets ' /proc/self/mountinfo; then tar -C /run/canvas-secrets -cf - .; else mkdir -p /tmp/empty-secrets && tar -C /tmp/empty-secrets -cf - .; fi"
	if err := m.compose(m.composePath(), version, m.config.StepTimeout, secretsEntry, "run", "--rm", "--no-deps", "--entrypoint", "sh", "--user", "root", "postgres", "-c", secretsCommand); err != nil {
		return Backup{}, fmt.Errorf("备份部署密钥卷：%w", err)
	}
	if err := archive.Close(); err != nil {
		return Backup{}, fmt.Errorf("完成 ZIP 备份：%w", err)
	}
	if err := file.Sync(); err != nil {
		return Backup{}, fmt.Errorf("同步 ZIP 备份：%w", err)
	}
	if err := file.Close(); err != nil {
		return Backup{}, err
	}
	if err := os.Rename(temporary, path); err != nil {
		return Backup{}, fmt.Errorf("提交 ZIP 备份：%w", err)
	}
	removeTemporary = false
	stat, err := os.Stat(path)
	if err != nil {
		return Backup{}, err
	}
	checksum := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if err := verifyZipBackup(path, checksum, backupArchiveLimit(m.config.MigrationMaxBytes)); err != nil {
		return Backup{}, err
	}
	return Backup{ID: id, Path: path, Checksum: checksum, Size: stat.Size(), CreatedAt: now, Version: version}, nil
}

func writeZipBytes(writer *zip.Writer, name string, data []byte) error {
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		return err
	}
	_, err = entry.Write(data)
	return err
}

func writeZipBytesFromFile(writer *zip.Writer, name, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return writeZipBytes(writer, name, data)
}

func verifyZipBackup(path, expected string, maxBytes int64) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	actual := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if actual != expected {
		return fmt.Errorf("ZIP 备份校验失败：期望 %s，实际 %s", expected, actual)
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("ZIP 备份无法打开：%w", err)
	}
	defer archive.Close()
	required := map[string]bool{"metadata.json": false, "database.dump": false, "backend-data.tar": false}
	entries := make(map[string]*zip.File, len(archive.File))
	for _, entry := range archive.File {
		entries[entry.Name] = entry
		if _, ok := required[entry.Name]; ok && entry.UncompressedSize64 > 0 {
			required[entry.Name] = true
		}
	}
	if metadataEntry := entries["metadata.json"]; metadataEntry != nil {
		metadata, metadataErr := readZipFile(metadataEntry, 1<<20)
		if metadataErr != nil {
			return fmt.Errorf("读取备份元数据：%w", metadataErr)
		}
		var value struct {
			Format int `json:"format"`
		}
		if err := json.Unmarshal(metadata, &value); err != nil {
			return fmt.Errorf("解析备份元数据：%w", err)
		}
		if value.Format >= 2 {
			required["service/.env"] = false
			required["service/compose.yml"] = false
			required["redis-data.tar"] = false
			required["deployment-secrets.tar"] = false
			for _, name := range []string{"service/.env", "service/compose.yml", "redis-data.tar", "deployment-secrets.tar"} {
				if entry := entries[name]; entry != nil && entry.UncompressedSize64 > 0 {
					required[name] = true
				}
			}
			if dump := entries["database.dump"]; dump != nil {
				prefix, prefixErr := readZipPrefix(dump, 5)
				if prefixErr != nil || string(prefix) != "PGDMP" {
					return errors.New("PostgreSQL 备份不是有效的 custom-format dump")
				}
			}
			for _, name := range []string{"backend-data.tar", "redis-data.tar", "deployment-secrets.tar"} {
				if entry := entries[name]; entry != nil {
					if err := validateMigrationTar(entry, backupArchiveLimit(maxBytes)); err != nil {
						return fmt.Errorf("备份文件 %s 无效：%w", name, err)
					}
				}
			}
		}
	}
	for name, present := range required {
		if !present {
			return fmt.Errorf("ZIP 备份缺少有效文件 %s", name)
		}
	}
	return nil
}

func backupArchiveLimit(limit int64) int64 {
	if limit <= 0 {
		return defaultArchiveMaxBytes
	}
	return limit
}

func readZipPrefix(entry *zip.File, size int64) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, size))
}

func (m *Manager) restoreDatabase(backup Backup) error {
	if err := verifyZipBackup(backup.Path, backup.Checksum, backupArchiveLimit(m.config.MigrationMaxBytes)); err != nil {
		return fmt.Errorf("拒绝恢复未通过校验的备份：%w", err)
	}
	archive, err := zip.OpenReader(backup.Path)
	if err != nil {
		return err
	}
	defer archive.Close()
	var dump *zip.File
	for _, entry := range archive.File {
		if entry.Name == "database.dump" {
			dump = entry
			break
		}
	}
	if dump == nil {
		return errors.New("备份中不存在 database.dump")
	}
	reader, err := dump.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	values, err := readEnvFile(m.envPath())
	if err != nil {
		return err
	}
	postgresUser := firstNonEmpty(values["POSTGRES_USER"], "open_ai_canvas")
	postgresDB := firstNonEmpty(values["POSTGRES_DB"], "open_ai_canvas")
	if postgresDB == "postgres" || strings.HasPrefix(postgresDB, "template") {
		return fmt.Errorf("拒绝覆盖 PostgreSQL 系统数据库 %q", postgresDB)
	}
	if err := m.compose(m.composePath(), backup.Version, 5*time.Minute, nil, "exec", "-T", "postgres", "dropdb", "-U", postgresUser, "--if-exists", "--force", postgresDB); err != nil {
		return fmt.Errorf("删除待恢复数据库：%w", err)
	}
	if err := m.compose(m.composePath(), backup.Version, 5*time.Minute, nil, "exec", "-T", "postgres", "createdb", "-U", postgresUser, "-O", postgresUser, postgresDB); err != nil {
		return fmt.Errorf("重建待恢复数据库：%w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.config.StepTimeout)
	defer cancel()
	args := []string{"compose", "--env-file", m.envPath(), "-f", m.composePath(), "exec", "-T", "postgres", "pg_restore", "-U", postgresUser, "-d", postgresDB, "--no-owner", "--no-privileges"}
	var stderr bytes.Buffer
	command := execCommandWithInput{runner: m.runner, input: reader}
	if err := command.Run(ctx, "docker", args, []string{"CANVAS_IMAGE_TAG=" + strings.TrimPrefix(backup.Version, "v")}, io.Discard, &stderr); err != nil {
		return fmt.Errorf("恢复 PostgreSQL：%s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (m *Manager) restoreBackendData(backup Backup) error {
	return m.restoreBackupDataEntry(backup, "backend-data.tar", "backend")
}

func (m *Manager) restoreRedisData(backup Backup) error {
	present, err := zipContainsEntry(backup.Path, "redis-data.tar")
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	if err := m.compose(m.composePath(), backup.Version, 2*time.Minute, nil, "stop", "redis"); err != nil {
		return fmt.Errorf("停止 Redis：%w", err)
	}
	return m.restoreBackupDataEntry(backup, "redis-data.tar", "redis")
}

func (m *Manager) restoreDeploymentSecrets(backup Backup) error {
	present, err := zipContainsEntry(backup.Path, "deployment-secrets.tar")
	if err != nil || !present {
		return err
	}
	archive, err := zip.OpenReader(backup.Path)
	if err != nil {
		return err
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.Name != "deployment-secrets.tar" {
			continue
		}
		if err := validateMigrationTar(entry, backupArchiveLimit(m.config.MigrationMaxBytes)); err != nil {
			return err
		}
		hasFiles, err := migrationTarHasFiles(entry)
		if err != nil {
			return err
		}
		if !hasFiles {
			return nil
		}
		return m.restoreMigrationEntry(entry, func(reader io.Reader) error {
			command := "grep -qs ' /run/canvas-secrets ' /proc/self/mountinfo || { echo 'deployment-secrets volume is not mounted' >&2; exit 1; }; find /run/canvas-secrets -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + && tar -C /run/canvas-secrets -xf -"
			return m.composeWithInput(m.composePath(), backup.Version, m.config.StepTimeout, reader, "run", "--rm", "--no-deps", "--entrypoint", "sh", "--user", "root", "postgres", "-c", command)
		})
	}
	return nil
}

func migrationTarHasFiles(entry *zip.File) (bool, error) {
	reader, err := entry.Open()
	if err != nil {
		return false, err
	}
	defer reader.Close()
	archive := tar.NewReader(reader)
	for {
		header, nextErr := archive.Next()
		if errors.Is(nextErr, io.EOF) {
			return false, nil
		}
		if nextErr != nil {
			return false, nextErr
		}
		if strings.TrimPrefix(header.Name, "./") != "" {
			return true, nil
		}
	}
}

func zipContainsEntry(archivePath, entryName string) (bool, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return false, err
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.Name == entryName {
			return true, nil
		}
	}
	return false, nil
}

func (m *Manager) restoreBackupDataEntry(backup Backup, entryName, service string) error {
	archive, err := zip.OpenReader(backup.Path)
	if err != nil {
		return err
	}
	defer archive.Close()
	var dataEntry *zip.File
	for _, entry := range archive.File {
		if entry.Name == entryName {
			dataEntry = entry
			break
		}
	}
	if dataEntry == nil {
		return fmt.Errorf("备份中不存在 %s", entryName)
	}
	if err := validateMigrationTar(dataEntry, backupArchiveLimit(m.config.MigrationMaxBytes)); err != nil {
		return fmt.Errorf("%s 校验失败：%w", entryName, err)
	}
	return m.restoreMigrationEntry(dataEntry, func(reader io.Reader) error {
		command := "find /data -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + && tar -C /data -xf -"
		return m.composeWithInput(m.composePath(), backup.Version, m.config.StepTimeout, reader, "run", "--rm", "--no-deps", "--entrypoint", "sh", "--user", "root", service, "-c", command)
	})
}

func restoreBackupServiceFile(backupPath, entryName, targetPath string) (bool, error) {
	archive, err := zip.OpenReader(backupPath)
	if err != nil {
		return false, err
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.Name != entryName {
			continue
		}
		data, readErr := readZipFile(entry, 8<<20)
		if readErr != nil {
			return false, readErr
		}
		return true, writeMigrationFile(targetPath, data)
	}
	return false, nil
}

type execCommandWithInput struct {
	runner commandRunner
	input  io.Reader
}

func (c execCommandWithInput) Run(ctx context.Context, name string, args, environment []string, stdout, stderr io.Writer) error {
	runner, ok := c.runner.(commandInputRunner)
	if !ok {
		return errors.New("当前命令执行器不支持标准输入")
	}
	return runner.RunWithInput(ctx, name, args, environment, c.input, stdout, stderr)
}

func (m *Manager) verifyHealthy(targetVersion string) error {
	healthURL := m.healthURL()
	deadline := time.Now().Add(10 * time.Minute)
	stableSince := time.Time{}
	for time.Now().Before(deadline) {
		if err := m.checkHealthOnce(healthURL, targetVersion); err != nil {
			stableSince = time.Time{}
		} else if stableSince.IsZero() {
			stableSince = time.Now()
		} else if time.Since(stableSince) >= m.config.StableWindow {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("目标版本 %s 未在健康检查窗口内稳定就绪", targetVersion)
}

func (m *Manager) healthURL() string {
	if configured := strings.TrimSpace(m.config.HealthURL); configured != "" {
		return configured
	}
	values, err := readEnvFile(m.envPath())
	if err != nil {
		return "http://127.0.0.1:3000/api/health/ready"
	}
	return "http://127.0.0.1:" + firstNonEmpty(values["CANVAS_HTTP_PORT"], "3000") + "/api/health/ready"
}

func (m *Manager) checkHealthOnce(healthURL, targetVersion string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	response, err := m.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("健康接口返回 HTTP %d", response.StatusCode)
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Build struct {
				Version string `json:"version"`
			} `json:"build"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(&payload); err != nil {
		return err
	}
	if payload.Code != 0 {
		return errors.New("健康接口业务状态异常")
	}
	if payload.Data.Build.Version != "" && CompareVersions(payload.Data.Build.Version, targetVersion) != 0 {
		return fmt.Errorf("运行版本仍为 %s，期望 %s", payload.Data.Build.Version, targetVersion)
	}
	return nil
}

func readEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s：%w", path, err)
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"")
		}
	}
	return values, nil
}

func setEnvValue(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	found := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+"=") {
			lines[index] = key + "=" + value
			found = true
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".env-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(stat.Mode().Perm()); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return err
	}
	return os.Chmod(path, stat.Mode().Perm())
}

func replaceFile(source, target string, mode os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if mode == 0 {
		if stat, statErr := os.Stat(target); statErr == nil {
			mode = stat.Mode().Perm()
		} else {
			mode = 0o600
		}
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".compose-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, target)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
