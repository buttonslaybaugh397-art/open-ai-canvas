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
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const migrationArchiveFormat = 1

var migrationProtectedEnvKeys = []string{
	"POSTGRES_DB",
	"POSTGRES_USER",
	"POSTGRES_PASSWORD",
	"DATABASE_URL",
	"REDIS_URL",
	"CANVAS_DATA_PATH",
	"CANVAS_POSTGRES_DATA_PATH",
	"CANVAS_REDIS_DATA_PATH",
	"CANVAS_HTTP_PORT",
	"CANVAS_UPDATER_TOKEN",
	"CANVAS_UPDATER_TOKEN_FILE",
	"CANVAS_UPDATER_SOCKET",
	"CANVAS_UPDATER_SOCKET_DIR",
	"CANVAS_UPDATER_INSTALL_DIR",
	"CANVAS_UPDATER_STATE_DIR",
	"CANVAS_UPDATER_BACKUP_DIR",
	"CANVAS_UPDATER_COMPOSE_FILE",
	"CANVAS_UPDATER_RELEASE_COMPOSE_FILE",
	"CANVAS_UPDATER_CONFIG_SOURCE",
	"CANVAS_UPDATER_REPOSITORY",
	"CANVAS_UPDATER_HEALTH_URL",
	"COMPOSE_PROJECT_NAME",
}

type migrationManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type migrationManifest struct {
	SchemaVersion  int                     `json:"schemaVersion"`
	ID             string                  `json:"id"`
	CreatedAt      time.Time               `json:"createdAt"`
	Version        string                  `json:"version"`
	DatabaseDriver string                  `json:"databaseDriver"`
	Files          []migrationManifestFile `json:"files"`
}

type migrationSource struct {
	archive  *zip.ReadCloser
	metadata MigrationArchive
	files    map[string]*zip.File
}

func (m *Manager) StartMigrationExport() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Operation.Phase.Active() {
		return m.snapshotLocked(), errors.New("系统更新正在进行，暂时不能导出迁移包")
	}
	if m.state.Migration.Phase.Active() {
		return m.snapshotLocked(), errors.New("已有数据迁移操作正在进行")
	}
	version, err := m.requireMigrationSupportLocked()
	if err != nil {
		return m.snapshotLocked(), err
	}
	m.beginMigrationLocked("export", MigrationPhaseValidating, "开始检查迁移导出环境")
	go m.runMigrationExport(version)
	return m.snapshotLocked(), nil
}

func (m *Manager) AcceptMigrationImport(contentLength int64, source io.Reader) (Status, error) {
	if source == nil {
		return Status{}, errors.New("迁移包内容不能为空")
	}
	m.mu.Lock()
	if m.state.Operation.Phase.Active() {
		status := m.snapshotLocked()
		m.mu.Unlock()
		return status, errors.New("系统更新正在进行，暂时不能导入迁移包")
	}
	if m.state.Migration.Phase.Active() {
		status := m.snapshotLocked()
		m.mu.Unlock()
		return status, errors.New("已有数据迁移操作正在进行")
	}
	if _, err := m.requireMigrationSupportLocked(); err != nil {
		status := m.snapshotLocked()
		m.mu.Unlock()
		return status, err
	}
	if contentLength <= 0 {
		status := m.snapshotLocked()
		m.mu.Unlock()
		return status, errors.New("迁移包大小无效")
	}
	if contentLength > m.config.MigrationMaxBytes {
		status := m.snapshotLocked()
		m.mu.Unlock()
		return status, fmt.Errorf("迁移包超过允许大小 %s", formatMigrationBytes(m.config.MigrationMaxBytes))
	}
	m.beginMigrationLocked("import", MigrationPhaseUploading, "正在接收迁移包")
	m.mu.Unlock()

	path, err := m.stageMigrationArchive(contentLength, source)
	if err != nil {
		m.finishMigrationFailure(err)
		return m.Snapshot(), err
	}
	m.setMigrationPhase(MigrationPhaseValidating, "正在校验迁移包清单与 SHA-256")
	loaded, err := m.openMigrationArchive(path)
	if err != nil {
		_ = os.Remove(path)
		m.finishMigrationFailure(err)
		return m.Snapshot(), err
	}
	loaded.archive.Close()
	m.mu.Lock()
	m.state.Migration.Archive = cloneMigrationArchive(&loaded.metadata)
	m.appendMigrationLogLocked(MigrationPhaseValidating, "迁移包校验通过，准备创建恢复前备份")
	_ = m.saveStateLocked()
	m.mu.Unlock()
	go m.runMigrationImport(path, loaded.metadata)
	return m.Snapshot(), nil
}

func (m *Manager) OpenMigrationExport() (MigrationArchive, io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.LastMigrationExport == nil {
		return MigrationArchive{}, nil, errors.New("尚未生成可下载的迁移包")
	}
	archive := *m.state.LastMigrationExport
	if err := verifyMigrationFileChecksum(archive.Path, archive.Checksum); err != nil {
		return MigrationArchive{}, nil, fmt.Errorf("迁移包校验失败，拒绝下载：%w", err)
	}
	file, err := os.Open(archive.Path)
	if err != nil {
		return MigrationArchive{}, nil, err
	}
	return *cloneMigrationArchive(&archive), file, nil
}

func (m *Manager) runMigrationExport(version string) {
	wasRunning, err := m.stopMigrationServices(version)
	if err != nil {
		m.finishMigrationFailure(err)
		return
	}
	m.setMigrationPhase(MigrationPhasePackaging, "正在打包数据库、缓存、文件、配置和服务镜像")
	archive, err := m.createMigrationArchive(version)
	if err == nil && wasRunning {
		m.setMigrationPhase(MigrationPhaseStarting, "正在恢复导出前运行的服务")
		err = m.startMigrationServices(version)
	}
	if err != nil {
		if wasRunning {
			_ = m.startMigrationServices(version)
		}
		m.finishMigrationFailure(err)
		return
	}
	m.mu.Lock()
	now := time.Now().UTC()
	m.state.LastMigrationExport = &archive
	m.state.Migration.Phase = MigrationPhaseSucceeded
	m.state.Migration.FinishedAt = &now
	m.state.Migration.Error = ""
	m.state.Migration.Archive = cloneMigrationArchive(&archive)
	m.appendMigrationLogLocked(MigrationPhaseSucceeded, "迁移包已生成并完成 SHA-256 校验")
	_ = m.saveStateLocked()
	m.mu.Unlock()
}

func (m *Manager) runMigrationImport(sourcePath string, sourceMetadata MigrationArchive) {
	defer os.Remove(sourcePath)
	currentVersion, err := m.currentVersion()
	if err != nil {
		m.finishMigrationFailure(err)
		return
	}
	wasRunning, err := m.stopMigrationServices(currentVersion)
	if err != nil {
		m.finishMigrationFailure(err)
		return
	}
	m.setMigrationPhase(MigrationPhaseBackingUp, "正在创建导入前完整恢复包")
	rollbackArchive, err := m.createMigrationArchive(currentVersion)
	if err != nil {
		if wasRunning {
			_ = m.startMigrationServices(currentVersion)
		}
		m.finishMigrationFailure(err)
		return
	}
	m.setMigrationPhase(MigrationPhaseRestoring, "正在恢复数据库、缓存、文件、配置和服务镜像")
	if err := m.restoreMigrationArchive(sourcePath, true); err != nil {
		m.setMigrationPhase(MigrationPhaseRestoring, "迁移恢复失败，正在恢复导入前状态")
		rollbackErr := m.restoreMigrationArchive(rollbackArchive.Path, false)
		if rollbackErr == nil && wasRunning {
			rollbackErr = m.startMigrationServices(currentVersion)
		}
		if rollbackErr != nil {
			m.finishMigrationManual(fmt.Errorf("导入失败：%v；自动恢复导入前状态失败：%w", err, rollbackErr))
			return
		}
		m.finishMigrationFailure(fmt.Errorf("导入失败，已自动恢复导入前状态：%w", err))
		return
	}
	m.setMigrationPhase(MigrationPhaseStarting, "正在启动恢复后的服务")
	if err := m.startMigrationServices(sourceMetadata.Version); err != nil {
		m.setMigrationPhase(MigrationPhaseRestoring, "服务启动失败，正在恢复导入前状态")
		rollbackErr := m.restoreMigrationArchive(rollbackArchive.Path, false)
		if rollbackErr == nil && wasRunning {
			rollbackErr = m.startMigrationServices(currentVersion)
		}
		if rollbackErr != nil {
			m.finishMigrationManual(fmt.Errorf("恢复后的服务无法启动：%v；自动恢复导入前状态失败：%w", err, rollbackErr))
			return
		}
		m.finishMigrationFailure(fmt.Errorf("恢复后的服务无法启动，已自动恢复导入前状态：%w", err))
		return
	}
	m.setMigrationPhase(MigrationPhaseVerifying, "正在验证恢复后的服务健康状态")
	if err := m.verifyHealthy(sourceMetadata.Version); err != nil {
		m.setMigrationPhase(MigrationPhaseRestoring, "健康验证失败，正在恢复导入前状态")
		rollbackErr := m.restoreMigrationArchive(rollbackArchive.Path, false)
		if rollbackErr == nil && wasRunning {
			rollbackErr = m.startMigrationServices(currentVersion)
		}
		if rollbackErr != nil {
			m.finishMigrationManual(fmt.Errorf("恢复后的服务未通过健康检查：%v；自动恢复导入前状态失败：%w", err, rollbackErr))
			return
		}
		m.finishMigrationFailure(fmt.Errorf("恢复后的服务未通过健康检查，已自动恢复导入前状态：%w", err))
		return
	}
	m.mu.Lock()
	now := time.Now().UTC()
	m.state.Migration.Phase = MigrationPhaseSucceeded
	m.state.Migration.FinishedAt = &now
	m.state.Migration.Error = ""
	m.state.Migration.Archive = cloneMigrationArchive(&sourceMetadata)
	m.appendMigrationLogLocked(MigrationPhaseSucceeded, "数据与服务恢复完成，并已通过健康检查")
	_ = m.saveStateLocked()
	m.mu.Unlock()
}

func (m *Manager) createMigrationArchive(version string) (MigrationArchive, error) {
	values, err := readEnvFile(m.envPath())
	if err != nil {
		return MigrationArchive{}, err
	}
	if !isPostgresMigration(values) {
		return MigrationArchive{}, errors.New("后台全量迁移目前仅支持 PostgreSQL 部署")
	}
	images, err := m.migrationImages(version)
	if err != nil {
		return MigrationArchive{}, err
	}
	now := time.Now().UTC()
	id := "migration-" + now.Format("20060102-150405") + "-" + randomID()[:8]
	if err := os.MkdirAll(m.migrationDirectory(), 0o700); err != nil {
		return MigrationArchive{}, fmt.Errorf("创建迁移包目录：%w", err)
	}
	path := filepath.Join(m.migrationDirectory(), id+".zip")
	temporary := path + ".partial"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return MigrationArchive{}, fmt.Errorf("创建迁移包：%w", err)
	}
	removeTemporary := true
	defer func() {
		_ = file.Close()
		if removeTemporary {
			_ = os.Remove(temporary)
		}
	}()
	archive := zip.NewWriter(file)
	manifest := migrationManifest{SchemaVersion: migrationArchiveFormat, ID: id, CreatedAt: now, Version: version, DatabaseDriver: "postgres"}
	writeFile := func(name string, write func(io.Writer) error) error {
		entry, createErr := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if createErr != nil {
			return createErr
		}
		hash := sha256.New()
		counting := &migrationCountingWriter{writer: io.MultiWriter(entry, hash)}
		if writeErr := write(counting); writeErr != nil {
			return writeErr
		}
		manifest.Files = append(manifest.Files, migrationManifestFile{Path: name, Size: counting.size, SHA256: hex.EncodeToString(hash.Sum(nil))})
		return nil
	}
	if err := writeFile("metadata.json", func(writer io.Writer) error {
		data, marshalErr := json.MarshalIndent(map[string]any{"format": migrationArchiveFormat, "id": id, "createdAt": now, "version": version, "databaseDriver": "postgres"}, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		_, writeErr := writer.Write(data)
		return writeErr
	}); err != nil {
		return MigrationArchive{}, err
	}
	if err := writeFile("service/.env", func(writer io.Writer) error { return copyFileToWriter(m.envPath(), writer) }); err != nil {
		return MigrationArchive{}, err
	}
	if err := writeFile("service/compose.yml", func(writer io.Writer) error { return copyFileToWriter(m.composePath(), writer) }); err != nil {
		return MigrationArchive{}, err
	}
	postgresUser := firstNonEmpty(values["POSTGRES_USER"], "open_ai_canvas")
	postgresDB := firstNonEmpty(values["POSTGRES_DB"], "open_ai_canvas")
	if err := writeFile("database.dump", func(writer io.Writer) error {
		return m.compose(m.composePath(), version, m.config.StepTimeout, writer, "exec", "-T", "postgres", "pg_dump", "-U", postgresUser, "-d", postgresDB, "-Fc")
	}); err != nil {
		return MigrationArchive{}, fmt.Errorf("备份 PostgreSQL：%w", err)
	}
	if err := writeFile("backend-data.tar", func(writer io.Writer) error {
		return m.compose(m.composePath(), version, m.config.StepTimeout, writer, "run", "--rm", "--no-deps", "--entrypoint", "sh", "--user", "root", "backend", "-c", "tar -C /data -cf - .")
	}); err != nil {
		return MigrationArchive{}, fmt.Errorf("备份后端数据目录：%w", err)
	}
	if err := writeFile("redis-data.tar", func(writer io.Writer) error {
		if err := m.compose(m.composePath(), version, 2*time.Minute, nil, "exec", "-T", "redis", "redis-cli", "SAVE"); err != nil {
			return err
		}
		return m.compose(m.composePath(), version, m.config.StepTimeout, writer, "run", "--rm", "--no-deps", "--entrypoint", "sh", "--user", "root", "redis", "-c", "tar -C /data -cf - .")
	}); err != nil {
		return MigrationArchive{}, fmt.Errorf("备份 Redis 数据目录：%w", err)
	}
	if err := writeFile("images.tar", func(writer io.Writer) error {
		arguments := append([]string{"image", "save"}, images...)
		return m.docker(m.config.StepTimeout, writer, arguments...)
	}); err != nil {
		return MigrationArchive{}, fmt.Errorf("导出 Docker 镜像：%w", err)
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return MigrationArchive{}, err
	}
	if err := writeZipBytes(archive, "manifest.json", manifestData); err != nil {
		return MigrationArchive{}, err
	}
	if err := archive.Close(); err != nil {
		return MigrationArchive{}, fmt.Errorf("完成迁移包：%w", err)
	}
	if err := file.Sync(); err != nil {
		return MigrationArchive{}, err
	}
	if err := file.Close(); err != nil {
		return MigrationArchive{}, err
	}
	if err := os.Rename(temporary, path); err != nil {
		return MigrationArchive{}, err
	}
	removeTemporary = false
	checksum, size, err := migrationFileChecksum(path)
	if err != nil {
		return MigrationArchive{}, err
	}
	if size > m.config.MigrationMaxBytes {
		return MigrationArchive{}, fmt.Errorf("迁移包大小 %s 超过允许大小 %s", formatMigrationBytes(size), formatMigrationBytes(m.config.MigrationMaxBytes))
	}
	loaded, err := m.openMigrationArchive(path)
	if err != nil {
		return MigrationArchive{}, err
	}
	loaded.archive.Close()
	return MigrationArchive{ID: id, Path: path, Checksum: checksum, Size: size, CreatedAt: now, Version: version, DatabaseDriver: "postgres"}, nil
}

func (m *Manager) restoreMigrationArchive(archivePath string, preserveTargetInfrastructure bool) error {
	loaded, err := m.openMigrationArchive(archivePath)
	if err != nil {
		return err
	}
	defer loaded.archive.Close()
	currentEnv, err := os.ReadFile(m.envPath())
	if err != nil {
		return err
	}
	var localCompose composeDocument
	if preserveTargetInfrastructure && m.config.ManagedCompose {
		localCompose, err = m.readCompose(context.Background(), m.composePath(), "")
		if err != nil {
			return err
		}
	}
	// Redis must stop before its append-only files are replaced, otherwise it can
	// flush the pre-import in-memory state back over the restored files.
	m.setMigrationPhase(MigrationPhaseStopping, "正在停止 Redis，防止旧缓存覆盖恢复数据")
	if err := m.stopMigrationCache(); err != nil {
		return err
	}
	archiveEnv, err := readZipFile(loaded.files["service/.env"], m.config.MigrationMaxBytes)
	if err != nil {
		return err
	}
	composeData, err := readZipFile(loaded.files["service/compose.yml"], 8<<20)
	if err != nil {
		return err
	}
	restoredEnv := archiveEnv
	if preserveTargetInfrastructure {
		restoredEnv = mergeMigrationEnv(archiveEnv, currentEnv)
	}
	if err := writeMigrationFile(m.envPath(), restoredEnv); err != nil {
		return fmt.Errorf("恢复部署环境：%w", err)
	}
	if err := writeMigrationFile(m.composePath(), composeData); err != nil {
		return fmt.Errorf("恢复 Compose 配置：%w", err)
	}
	if localCompose != nil {
		restoredCompose, err := m.readCompose(context.Background(), m.composePath(), loaded.metadata.Version)
		if err != nil {
			return err
		}
		restoredCompose = preserveMigrationDeployment(restoredCompose, localCompose)
		if err := pinComposeImages(restoredCompose, loaded.metadata.Version); err != nil {
			return err
		}
		if _, err := describeDeployment(restoredCompose, m.config.Repository); err != nil {
			return err
		}
		if err := m.writeCompose(context.Background(), restoredCompose, m.composePath(), false); err != nil {
			return err
		}
	}
	if err := m.syncComposeConfig(context.Background()); err != nil {
		return err
	}
	if err := m.compose(m.composePath(), loaded.metadata.Version, 2*time.Minute, nil, "config", "--quiet"); err != nil {
		return fmt.Errorf("恢复后的 Compose 配置无效：%w", err)
	}
	if err := m.restoreMigrationEntry(loaded.files["images.tar"], func(reader io.Reader) error {
		return m.dockerWithInput(m.config.StepTimeout, reader, "image", "load")
	}); err != nil {
		return fmt.Errorf("导入 Docker 镜像：%w", err)
	}
	if err := m.restorePostgresDump(loaded.files["database.dump"], loaded.metadata.Version); err != nil {
		return err
	}
	if err := m.restoreTarData(loaded.files["backend-data.tar"], loaded.metadata.Version, "backend"); err != nil {
		return fmt.Errorf("恢复后端数据目录：%w", err)
	}
	if err := m.restoreTarData(loaded.files["redis-data.tar"], loaded.metadata.Version, "redis"); err != nil {
		return fmt.Errorf("恢复 Redis 数据目录：%w", err)
	}
	return nil
}

func (m *Manager) restorePostgresDump(entry *zip.File, version string) error {
	values, err := readEnvFile(m.envPath())
	if err != nil {
		return err
	}
	postgresUser := firstNonEmpty(values["POSTGRES_USER"], "open_ai_canvas")
	postgresDB := firstNonEmpty(values["POSTGRES_DB"], "open_ai_canvas")
	if postgresDB == "postgres" || strings.HasPrefix(postgresDB, "template") {
		return fmt.Errorf("拒绝覆盖 PostgreSQL 系统数据库 %q", postgresDB)
	}
	if err := m.compose(m.composePath(), version, 5*time.Minute, nil, "exec", "-T", "postgres", "dropdb", "-U", postgresUser, "--if-exists", "--force", postgresDB); err != nil {
		return fmt.Errorf("删除待恢复数据库：%w", err)
	}
	if err := m.compose(m.composePath(), version, 5*time.Minute, nil, "exec", "-T", "postgres", "createdb", "-U", postgresUser, "-O", postgresUser, postgresDB); err != nil {
		return fmt.Errorf("重建待恢复数据库：%w", err)
	}
	return m.restoreMigrationEntry(entry, func(reader io.Reader) error {
		return m.composeWithInput(m.composePath(), version, m.config.StepTimeout, reader, "exec", "-T", "postgres", "pg_restore", "-U", postgresUser, "-d", postgresDB, "--no-owner", "--no-privileges")
	})
}

func (m *Manager) restoreTarData(entry *zip.File, version, service string) error {
	if err := validateMigrationTar(entry, m.config.MigrationMaxBytes); err != nil {
		return err
	}
	return m.restoreMigrationEntry(entry, func(reader io.Reader) error {
		command := "find /data -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + && tar -C /data -xf -"
		return m.composeWithInput(m.composePath(), version, m.config.StepTimeout, reader, "run", "--rm", "--no-deps", "--entrypoint", "sh", "--user", "root", service, "-c", command)
	})
}

func (m *Manager) restoreMigrationEntry(entry *zip.File, restore func(io.Reader) error) error {
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	return restore(reader)
}

func (m *Manager) stopMigrationServices(version string) (bool, error) {
	running, err := m.migrationServicesRunning(version)
	if err != nil {
		return false, err
	}
	if !running {
		return false, nil
	}
	m.setMigrationPhase(MigrationPhaseStopping, "正在停止 Web 与 Backend，等待写入操作结束")
	if err := m.compose(m.composePath(), version, m.config.StepTimeout, nil, "stop", "web", "backend"); err != nil {
		return false, err
	}
	return true, nil
}

func (m *Manager) stopMigrationCache() error {
	version, err := m.currentVersion()
	if err != nil {
		return err
	}
	if err := m.compose(m.composePath(), version, m.config.StepTimeout, nil, "stop", "redis"); err != nil {
		return fmt.Errorf("停止 Redis：%w", err)
	}
	return nil
}

func (m *Manager) startMigrationServices(version string) error {
	return m.startApplicationServices(version)
}

func (m *Manager) migrationServicesRunning(version string) (bool, error) {
	var output bytes.Buffer
	if err := m.compose(m.composePath(), version, 30*time.Second, &output, "ps", "--status", "running", "-q", "backend", "web"); err != nil {
		return false, err
	}
	return strings.TrimSpace(output.String()) != "", nil
}

func (m *Manager) migrationImages(version string) ([]string, error) {
	var output bytes.Buffer
	if err := m.compose(m.composePath(), version, 2*time.Minute, &output, "config", "--images"); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	images := make([]string, 0)
	for _, line := range strings.Split(output.String(), "\n") {
		image := strings.TrimSpace(line)
		if image == "" {
			continue
		}
		if _, ok := seen[image]; ok {
			continue
		}
		seen[image] = struct{}{}
		images = append(images, image)
	}
	if len(images) == 0 {
		return nil, errors.New("当前 Compose 未找到可导出的服务镜像")
	}
	sort.Strings(images)
	return images, nil
}

func (m *Manager) stageMigrationArchive(contentLength int64, source io.Reader) (string, error) {
	directory := filepath.Join(m.config.StateDir, "migration-uploads")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(directory, ".upload-*.zip.partial")
	if err != nil {
		return "", err
	}
	path := file.Name()
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", err
	}
	written, err := io.Copy(file, io.LimitReader(source, m.config.MigrationMaxBytes+1))
	if err != nil {
		return "", err
	}
	if written != contentLength {
		return "", errors.New("迁移包传输不完整")
	}
	if written > m.config.MigrationMaxBytes {
		return "", fmt.Errorf("迁移包超过允许大小 %s", formatMigrationBytes(m.config.MigrationMaxBytes))
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	finalPath := strings.TrimSuffix(path, ".partial")
	if err := os.Rename(path, finalPath); err != nil {
		return "", err
	}
	remove = false
	return finalPath, nil
}

func (m *Manager) openMigrationArchive(archivePath string) (*migrationSource, error) {
	checksum, size, err := migrationFileChecksum(archivePath)
	if err != nil {
		return nil, err
	}
	if size <= 0 || size > m.config.MigrationMaxBytes {
		return nil, fmt.Errorf("迁移包大小无效：%s", formatMigrationBytes(size))
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("打开迁移包：%w", err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = archive.Close()
		}
	}()
	if len(archive.File) == 0 || len(archive.File) > 32 {
		return nil, errors.New("迁移包文件数量无效")
	}
	files := make(map[string]*zip.File, len(archive.File))
	var uncompressed uint64
	for _, entry := range archive.File {
		if err := safeMigrationZipEntry(entry); err != nil {
			return nil, err
		}
		if _, exists := files[entry.Name]; exists {
			return nil, fmt.Errorf("迁移包包含重复文件 %s", entry.Name)
		}
		files[entry.Name] = entry
		uncompressed += entry.UncompressedSize64
		if uncompressed > uint64(m.config.MigrationMaxBytes)*4 {
			return nil, errors.New("迁移包解压后体积异常")
		}
	}
	manifestEntry := files["manifest.json"]
	if manifestEntry == nil {
		return nil, errors.New("迁移包缺少 manifest.json")
	}
	manifestData, err := readZipFile(manifestEntry, 2<<20)
	if err != nil {
		return nil, err
	}
	var manifest migrationManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("解析迁移包清单：%w", err)
	}
	if manifest.SchemaVersion != migrationArchiveFormat || manifest.ID == "" || manifest.Version == "" || manifest.DatabaseDriver != "postgres" {
		return nil, errors.New("迁移包元数据不受支持")
	}
	required := map[string]bool{"metadata.json": false, "service/.env": false, "service/compose.yml": false, "database.dump": false, "backend-data.tar": false, "redis-data.tar": false, "images.tar": false}
	listed := make(map[string]migrationManifestFile, len(manifest.Files))
	for _, item := range manifest.Files {
		if !safeMigrationName(item.Path) || item.Size <= 0 || len(item.SHA256) != 64 {
			return nil, errors.New("迁移包清单包含无效文件")
		}
		if _, exists := listed[item.Path]; exists {
			return nil, fmt.Errorf("迁移包清单包含重复文件 %s", item.Path)
		}
		entry := files[item.Path]
		if entry == nil || int64(entry.UncompressedSize64) != item.Size {
			return nil, fmt.Errorf("迁移包文件缺失或大小不匹配：%s", item.Path)
		}
		actual, hashErr := hashZipFile(entry)
		if hashErr != nil {
			return nil, hashErr
		}
		if !strings.EqualFold(actual, item.SHA256) {
			return nil, fmt.Errorf("迁移包 SHA-256 校验失败：%s", item.Path)
		}
		if _, ok := required[item.Path]; ok {
			required[item.Path] = true
		}
		listed[item.Path] = item
	}
	for name, present := range required {
		if !present {
			return nil, fmt.Errorf("迁移包缺少必需文件 %s", name)
		}
	}
	if len(listed)+1 != len(files) {
		return nil, errors.New("迁移包包含未列入清单的文件")
	}
	metadataData, err := readZipFile(files["metadata.json"], 1<<20)
	if err != nil {
		return nil, err
	}
	var metadata struct {
		Format         int       `json:"format"`
		ID             string    `json:"id"`
		CreatedAt      time.Time `json:"createdAt"`
		Version        string    `json:"version"`
		DatabaseDriver string    `json:"databaseDriver"`
	}
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		return nil, fmt.Errorf("解析迁移包元数据：%w", err)
	}
	if metadata.Format != migrationArchiveFormat || metadata.ID != manifest.ID || metadata.Version != manifest.Version || metadata.DatabaseDriver != manifest.DatabaseDriver || metadata.CreatedAt.IsZero() {
		return nil, errors.New("迁移包元数据与清单不一致")
	}
	if err := validateMigrationTar(files["backend-data.tar"], m.config.MigrationMaxBytes); err != nil {
		return nil, fmt.Errorf("后端数据归档无效：%w", err)
	}
	if err := validateMigrationTar(files["redis-data.tar"], m.config.MigrationMaxBytes); err != nil {
		return nil, fmt.Errorf("Redis 数据归档无效：%w", err)
	}
	closeOnError = false
	return &migrationSource{archive: archive, metadata: MigrationArchive{ID: metadata.ID, Path: archivePath, Checksum: checksum, Size: size, CreatedAt: metadata.CreatedAt, Version: metadata.Version, DatabaseDriver: metadata.DatabaseDriver}, files: files}, nil
}

func safeMigrationZipEntry(entry *zip.File) error {
	if entry == nil || !safeMigrationName(entry.Name) || strings.HasSuffix(entry.Name, "/") {
		return errors.New("迁移包包含不安全路径")
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		return errors.New("迁移包不允许符号链接")
	}
	return nil
}

func safeMigrationName(name string) bool {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return false
	}
	cleaned := path.Clean(name)
	return cleaned == name && cleaned != "." && !strings.HasPrefix(cleaned, "../")
}

func validateMigrationTar(entry *zip.File, limit int64) error {
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	archive := tar.NewReader(reader)
	var total int64
	for {
		header, nextErr := archive.Next()
		if errors.Is(nextErr, io.EOF) {
			return nil
		}
		if nextErr != nil {
			return nextErr
		}
		name := strings.TrimPrefix(header.Name, "./")
		if name != "" && !safeMigrationName(name) {
			return fmt.Errorf("包含不安全路径 %q", header.Name)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			return fmt.Errorf("不支持归档项类型 %q", header.Name)
		}
		if header.Size < 0 || header.Size > limit-total {
			return errors.New("归档解压后体积异常")
		}
		total += header.Size
		if _, copyErr := io.Copy(io.Discard, archive); copyErr != nil {
			return copyErr
		}
	}
}

func (m *Manager) requireMigrationSupportLocked() (string, error) {
	if err := m.syncComposeConfig(context.Background()); err != nil {
		return "", err
	}
	values, err := readEnvFile(m.envPath())
	if err != nil {
		return "", err
	}
	if !isPostgresMigration(values) {
		return "", errors.New("当前 Host Updater 仅支持 PostgreSQL 部署的数据迁移")
	}
	version, err := m.currentVersion()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(m.composePath()); err != nil {
		return "", fmt.Errorf("读取当前 Compose：%w", err)
	}
	if err := checkBackupDiskSpace(m.config.BackupDir); err != nil {
		return "", err
	}
	return version, nil
}

func (m *Manager) migrationSupported() bool {
	values, err := readEnvFile(m.envPath())
	return err == nil && isPostgresMigration(values)
}

func (m *Manager) migrationSupportReason() string {
	values, err := readEnvFile(m.envPath())
	if err != nil {
		return "无法读取部署环境"
	}
	if !isPostgresMigration(values) {
		return "后台全量迁移目前仅支持 PostgreSQL 部署"
	}
	return ""
}

func isPostgresMigration(values map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(values["CANVAS_DATABASE_DRIVER"]), "postgres")
}

func (m *Manager) beginMigrationLocked(kind string, phase MigrationPhase, message string) {
	now := time.Now().UTC()
	m.state.Migration = MigrationOperation{
		ID:        randomID(),
		Kind:      kind,
		Phase:     phase,
		StartedAt: &now,
		Logs:      []MigrationLog{{At: now, Phase: phase, Message: message}},
	}
	_ = m.saveStateLocked()
}

func (m *Manager) setMigrationPhase(phase MigrationPhase, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Migration.Phase = phase
	m.appendMigrationLogLocked(phase, message)
	_ = m.saveStateLocked()
}

func (m *Manager) finishMigrationFailure(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	m.state.Migration.Phase = MigrationPhaseFailed
	m.state.Migration.Error = safeOperationError(err)
	m.state.Migration.FinishedAt = &now
	m.appendMigrationLogLocked(MigrationPhaseFailed, "迁移操作失败")
	_ = m.saveStateLocked()
}

func (m *Manager) finishMigrationManual(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	m.state.Migration.Phase = MigrationPhaseManualAction
	m.state.Migration.Error = safeOperationError(err)
	m.state.Migration.FinishedAt = &now
	m.appendMigrationLogLocked(MigrationPhaseManualAction, "自动恢复未完成，需要人工介入")
	_ = m.saveStateLocked()
}

func (m *Manager) appendMigrationLogLocked(phase MigrationPhase, message string) {
	m.state.Migration.Logs = append(m.state.Migration.Logs, MigrationLog{At: time.Now().UTC(), Phase: phase, Message: message})
	if len(m.state.Migration.Logs) > 200 {
		m.state.Migration.Logs = append([]MigrationLog(nil), m.state.Migration.Logs[len(m.state.Migration.Logs)-200:]...)
	}
}

func (m *Manager) migrationDirectory() string {
	return filepath.Join(m.config.BackupDir, "migrations")
}

func (m *Manager) docker(timeout time.Duration, stdout io.Writer, arguments ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var stderr bytes.Buffer
	if stdout == nil {
		stdout = io.Discard
	}
	if err := m.runner.Run(ctx, "docker", arguments, nil, stdout, &stderr); err != nil {
		return commandFailure("docker "+strings.Join(arguments, " "), err, &stderr)
	}
	return nil
}

func (m *Manager) dockerWithInput(timeout time.Duration, input io.Reader, arguments ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var stderr bytes.Buffer
	command := execCommandWithInput{runner: m.runner, input: input}
	if err := command.Run(ctx, "docker", arguments, nil, io.Discard, &stderr); err != nil {
		return commandFailure("docker "+strings.Join(arguments, " "), err, &stderr)
	}
	return nil
}

func (m *Manager) composeWithInput(composePath, imageTag string, timeout time.Duration, input io.Reader, arguments ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := m.composeArguments(composePath)
	args = append(args, arguments...)
	var stderr bytes.Buffer
	command := execCommandWithInput{runner: m.runner, input: input}
	if err := command.Run(ctx, "docker", args, []string{"CANVAS_IMAGE_TAG=" + strings.TrimPrefix(imageTag, "v")}, io.Discard, &stderr); err != nil {
		return commandFailure("docker compose "+strings.Join(arguments, " "), err, &stderr)
	}
	return nil
}

func commandFailure(command string, err error, stderr *bytes.Buffer) error {
	message := strings.TrimSpace(stderr.String())
	if len(message) > 1000 {
		message = message[len(message)-1000:]
	}
	if message != "" {
		return fmt.Errorf("%s：%s", command, message)
	}
	return fmt.Errorf("%s：%w", command, err)
}

func copyFileToWriter(filePath string, writer io.Writer) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}

func readZipFile(entry *zip.File, limit int64) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("迁移包文件超过允许大小")
	}
	return data, nil
}

func hashZipFile(entry *zip.File) (string, error) {
	reader, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer reader.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func migrationFileChecksum(filePath string) (string, int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), size, nil
}

func verifyMigrationFileChecksum(filePath, expected string) error {
	actual, _, err := migrationFileChecksum(filePath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("期望 %s，实际 %s", expected, actual)
	}
	return nil
}

func mergeMigrationEnv(source, target []byte) []byte {
	protected := make(map[string]string, len(migrationProtectedEnvKeys))
	for _, line := range strings.Split(string(target), "\n") {
		key, value, ok := splitMigrationEnvLine(line)
		if ok {
			protected[key] = value
		}
	}
	wanted := make(map[string]struct{}, len(migrationProtectedEnvKeys))
	for _, key := range migrationProtectedEnvKeys {
		wanted[key] = struct{}{}
	}
	seen := make(map[string]bool, len(wanted))
	lines := strings.Split(strings.TrimSuffix(string(source), "\n"), "\n")
	for index, line := range lines {
		key, _, ok := splitMigrationEnvLine(line)
		if !ok {
			continue
		}
		if _, keep := wanted[key]; keep {
			if value, exists := protected[key]; exists {
				lines[index] = key + "=" + value
			}
			seen[key] = true
		}
	}
	for _, key := range migrationProtectedEnvKeys {
		if seen[key] {
			continue
		}
		if value, exists := protected[key]; exists {
			lines = append(lines, key+"="+value)
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func splitMigrationEnvLine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	key, value, ok := strings.Cut(trimmed, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return "", "", false
	}
	for index, character := range key {
		if !(character == '_' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || index > 0 && character >= '0' && character <= '9') {
			return "", "", false
		}
	}
	return key, value, true
}

func writeMigrationFile(filePath string, data []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(filePath); err == nil {
		mode = info.Mode().Perm()
	}
	temporary, err := os.CreateTemp(filepath.Dir(filePath), ".migration-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
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
	return os.Rename(temporaryPath, filePath)
}

type migrationCountingWriter struct {
	writer io.Writer
	size   int64
}

func (w *migrationCountingWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	w.size += int64(written)
	return written, err
}

func formatMigrationBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}
