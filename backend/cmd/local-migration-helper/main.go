package main

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"infinite-canvas/backend/internal/hostupdate"
)

const defaultMigrationMaxBytes int64 = 20 << 30

type localMigrationFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type localMigrationManifest struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	Version        string               `json:"version"`
	DatabaseDriver string               `json:"databaseDriver"`
	Files          []localMigrationFile `json:"files"`
}

type helper struct {
	mu        sync.Mutex
	root      string
	hostRoot  string
	script    string
	token     string
	maxBytes  int64
	last      *hostupdate.MigrationArchive
	operation hostupdate.MigrationOperation
}

func main() {
	root := flag.String("root", "", "open-ai-canvas repository root")
	// Docker Desktop reaches Windows services through host.docker.internal;
	// binding only to loopback would make the helper unreachable from backend.
	address := flag.String("address", "0.0.0.0:9714", "helper listen address")
	tokenFile := flag.String("token-file", "", "bearer token file")
	maxBytes := flag.Int64("max-bytes", defaultMigrationMaxBytes, "maximum migration archive size in bytes")
	flag.Parse()

	if strings.TrimSpace(*root) == "" || strings.TrimSpace(*tokenFile) == "" || *maxBytes <= 0 {
		log.Fatal("root, token-file, and a positive max-bytes value are required")
	}
	absoluteRoot, err := filepath.Abs(*root)
	if err != nil {
		log.Fatal(err)
	}
	scriptName := "migrate-local.sh"
	if runtime.GOOS == "windows" {
		scriptName = "migrate-local.ps1"
	}
	script := filepath.Join(absoluteRoot, "scripts", scriptName)
	if _, err := os.Stat(script); err != nil {
		log.Fatalf("local migration script is unavailable: %v", err)
	}
	tokenData, err := os.ReadFile(*tokenFile)
	if err != nil {
		log.Fatalf("read helper token: %v", err)
	}
	token := strings.TrimSpace(string(tokenData))
	if len(token) < 32 {
		log.Fatal("local migration helper token must contain at least 32 characters")
	}

	h := &helper{
		root:      absoluteRoot,
		hostRoot:  strings.TrimSpace(os.Getenv("CANVAS_HOST_REPOSITORY_ROOT")),
		script:    script,
		token:     token,
		maxBytes:  *maxBytes,
		operation: hostupdate.MigrationOperation{Phase: hostupdate.MigrationPhaseIdle, Logs: []hostupdate.MigrationLog{}},
	}
	if h.hostRoot == "" {
		h.hostRoot = discoverHostRepositoryRoot()
	}
	if archive, err := h.findLatestArchive(); err == nil {
		h.last = &archive
	}

	ctx, stop := signalContext()
	defer stop()
	server := &http.Server{
		Addr:              *address,
		Handler:           h.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	log.Printf("local migration helper listening on http://%s", *address)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown local migration helper: %v", err)
		}
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func (h *helper) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, _ *http.Request) { h.writeJSON(w, http.StatusOK, h.snapshot()) })
	mux.HandleFunc("POST /v1/migration/export", h.startExport)
	mux.HandleFunc("POST /v1/migration/import", h.startImport)
	mux.HandleFunc("GET /v1/migration/download", h.download)
	protected := h.authorize(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			mux.ServeHTTP(w, r)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

func (h *helper) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if len(provided) != len(h.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(h.token)) != 1 {
			h.writeError(w, http.StatusUnauthorized, errors.New("migration helper authentication failed"), nil)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (h *helper) startExport(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	if h.operation.Phase.Active() {
		status := h.snapshotLocked()
		h.mu.Unlock()
		h.writeError(w, http.StatusConflict, errors.New("a migration operation is already running"), status)
		return
	}
	h.beginLocked("export", hostupdate.MigrationPhaseValidating, "正在检查本地迁移导出环境")
	status := h.snapshotLocked()
	h.mu.Unlock()
	go h.runExport()
	h.writeJSON(w, http.StatusAccepted, status)
}

func (h *helper) startImport(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength <= 0 || r.ContentLength > h.maxBytes {
		h.writeError(w, http.StatusBadRequest, errors.New("迁移包大小无效或超过限制"), nil)
		return
	}
	h.mu.Lock()
	if h.operation.Phase.Active() {
		status := h.snapshotLocked()
		h.mu.Unlock()
		h.writeError(w, http.StatusConflict, errors.New("a migration operation is already running"), status)
		return
	}
	h.beginLocked("import", hostupdate.MigrationPhaseUploading, "正在接收迁移包")
	h.mu.Unlock()

	archivePath, err := h.stageUpload(w, r)
	if err == nil {
		h.setPhase(hostupdate.MigrationPhaseValidating, "正在校验迁移包清单与 SHA-256")
		err = validateLocalMigrationArchive(archivePath, h.maxBytes)
	}
	if err != nil {
		_ = os.Remove(archivePath)
		h.fail(err)
		h.writeError(w, http.StatusBadRequest, err, h.snapshot())
		return
	}
	h.mu.Lock()
	archive, archiveErr := h.archiveMetadata(archivePath)
	if archiveErr == nil {
		h.operation.Archive = &archive
		h.appendLogLocked(hostupdate.MigrationPhaseValidating, "迁移包校验通过，准备恢复")
	}
	status := h.snapshotLocked()
	h.mu.Unlock()
	if archiveErr != nil {
		_ = os.Remove(archivePath)
		h.fail(archiveErr)
		h.writeError(w, http.StatusBadRequest, archiveErr, h.snapshot())
		return
	}
	go h.runImport(archivePath, archive)
	h.writeJSON(w, http.StatusAccepted, status)
}

func (h *helper) download(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	archive := h.last
	if archive == nil {
		if found, err := h.findLatestArchive(); err == nil {
			archive = &found
			h.last = archive
		}
	}
	if archive != nil {
		copy := *archive
		archive = &copy
	}
	h.mu.Unlock()
	if archive == nil {
		h.writeError(w, http.StatusNotFound, errors.New("尚未生成可下载的迁移包"), nil)
		return
	}
	if err := validateLocalMigrationArchive(archive.Path, h.maxBytes); err != nil {
		h.writeError(w, http.StatusConflict, fmt.Errorf("迁移包校验失败：%w", err), nil)
		return
	}
	file, err := os.Open(archive.Path)
	if err != nil {
		h.writeError(w, http.StatusNotFound, err, nil)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", archive.Size))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.zip\"", safeDownloadName(archive.ID)))
	w.Header().Set("X-Migration-ID", archive.ID)
	w.Header().Set("X-Migration-SHA256", archive.Checksum)
	w.Header().Set("X-Migration-Version", archive.Version)
	_, _ = io.Copy(w, file)
}

func (h *helper) runExport() {
	h.setPhase(hostupdate.MigrationPhasePackaging, "正在打包 SQLite 数据、配置和本地 Docker 镜像")
	archivePath := filepath.Join(h.migrationDirectory(), "open-ai-canvas-migration-"+time.Now().UTC().Format("20060102-150405")+".zip")
	if err := h.runScript("export", archivePath); err != nil {
		h.fail(err)
		return
	}
	if err := validateLocalMigrationArchive(archivePath, h.maxBytes); err != nil {
		h.fail(fmt.Errorf("导出后校验迁移包：%w", err))
		return
	}
	archive, err := h.archiveMetadata(archivePath)
	if err != nil {
		h.fail(err)
		return
	}
	h.mu.Lock()
	h.last = &archive
	h.succeedLocked(archive, "迁移包已生成并通过校验")
	h.mu.Unlock()
}

func (h *helper) runImport(archivePath string, archive hostupdate.MigrationArchive) {
	defer os.Remove(archivePath)
	h.setPhase(hostupdate.MigrationPhaseBackingUp, "正在创建导入前本地恢复备份")
	h.setPhase(hostupdate.MigrationPhaseRestoring, "正在恢复 SQLite 数据、配置和 Docker 镜像")
	if err := h.runScript("import", archivePath); err != nil {
		h.fail(err)
		return
	}
	h.setPhase(hostupdate.MigrationPhaseVerifying, "正在验证恢复后的本地服务")
	if err := h.verifyLocalService(); err != nil {
		h.fail(err)
		return
	}
	h.mu.Lock()
	h.succeedLocked(archive, "本地数据与服务恢复完成，并已通过健康检查")
	h.mu.Unlock()
}

func (h *helper) verifyLocalService() error {
	client := &http.Client{Timeout: 10 * time.Second}
	serviceURL := strings.TrimRight(strings.TrimSpace(os.Getenv("CANVAS_LOCAL_SERVICE_URL")), "/")
	if serviceURL == "" {
		serviceURL = "http://127.0.0.1:3000"
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(serviceURL + "/api/health/ready")
		if err == nil {
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return errors.New("恢复后本地服务未能通过健康检查")
}

func (h *helper) runScript(action, archivePath string) error {
	if err := os.MkdirAll(h.migrationDirectory(), 0o700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		powershell, err := exec.LookPath("pwsh.exe")
		if err != nil {
			powershell, err = exec.LookPath("powershell.exe")
		}
		if err != nil {
			return errors.New("未找到 pwsh.exe 或 powershell.exe")
		}
		command = exec.CommandContext(ctx, powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", h.script, "-Action", action, "-ArchivePath", archivePath, "-TargetRoot", h.root)
	} else {
		bash, err := exec.LookPath("bash")
		if err != nil {
			return errors.New("未找到 bash，无法执行本地迁移脚本")
		}
		command = exec.CommandContext(ctx, bash, h.script, action, archivePath, h.root)
	}
	command.Dir = h.root
	command.Env = os.Environ()
	command.Env = append(command.Env, "CANVAS_LOCAL_MIGRATION_HELPER_CONTAINER=1")
	if h.hostRoot != "" {
		command.Env = append(command.Env, "CANVAS_HOST_REPOSITORY_ROOT="+h.hostRoot)
	}
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("本地迁移脚本超时：%w", ctx.Err())
	}
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if len(detail) > 1200 {
			detail = detail[len(detail)-1200:]
		}
		if detail != "" {
			return fmt.Errorf("本地迁移脚本失败：%s", detail)
		}
		return fmt.Errorf("本地迁移脚本失败：%w", err)
	}
	return nil
}

func discoverHostRepositoryRoot() string {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return ""
	}
	containerID, err := os.Hostname()
	if err != nil || strings.TrimSpace(containerID) == "" {
		return ""
	}
	format := `{{range .Mounts}}{{if eq .Destination "/workspace"}}{{.Source}}{{end}}{{end}}`
	output, err := exec.Command(docker, "inspect", "--format", format, containerID).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (h *helper) stageUpload(w http.ResponseWriter, r *http.Request) (string, error) {
	directory := filepath.Join(h.root, ".local", "migration-uploads")
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
	body := http.MaxBytesReader(w, r.Body, h.maxBytes+1)
	written, err := io.Copy(file, body)
	if err != nil {
		return "", err
	}
	if written != r.ContentLength || written > h.maxBytes {
		return "", errors.New("迁移包传输不完整或超过限制")
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

func (h *helper) archiveMetadata(archivePath string) (hostupdate.MigrationArchive, error) {
	info, err := os.Stat(archivePath)
	if err != nil {
		return hostupdate.MigrationArchive{}, err
	}
	if info.Size() <= 0 || info.Size() > h.maxBytes {
		return hostupdate.MigrationArchive{}, errors.New("迁移包大小无效")
	}
	checksum, err := fileChecksum(archivePath)
	if err != nil {
		return hostupdate.MigrationArchive{}, err
	}
	name := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))
	version := h.currentVersion()
	if archive, openErr := zip.OpenReader(archivePath); openErr == nil {
		defer archive.Close()
		for _, entry := range archive.File {
			if entry.Name != "manifest.json" {
				continue
			}
			if data, readErr := readZipEntry(entry, 2<<20); readErr == nil {
				var manifest localMigrationManifest
				if json.Unmarshal(data, &manifest) == nil && strings.TrimSpace(manifest.Version) != "" {
					version = strings.TrimSpace(manifest.Version)
				}
			}
			break
		}
	}
	return hostupdate.MigrationArchive{ID: name, Path: archivePath, Checksum: checksum, Size: info.Size(), CreatedAt: info.ModTime().UTC(), Version: version, DatabaseDriver: "sqlite"}, nil
}

func (h *helper) findLatestArchive() (hostupdate.MigrationArchive, error) {
	entries, err := os.ReadDir(h.migrationDirectory())
	if err != nil {
		return hostupdate.MigrationArchive{}, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".zip") || !strings.HasPrefix(entry.Name(), "open-ai-canvas-migration-") {
			continue
		}
		paths = append(paths, filepath.Join(h.migrationDirectory(), entry.Name()))
	}
	sort.Slice(paths, func(i, j int) bool {
		left, leftErr := os.Stat(paths[i])
		right, rightErr := os.Stat(paths[j])
		return leftErr == nil && (rightErr != nil || left.ModTime().After(right.ModTime()))
	})
	for _, archivePath := range paths {
		if err := validateLocalMigrationArchive(archivePath, h.maxBytes); err != nil {
			continue
		}
		return h.archiveMetadata(archivePath)
	}
	return hostupdate.MigrationArchive{}, errors.New("未找到可用迁移包")
}

func (h *helper) migrationDirectory() string { return filepath.Join(h.root, ".local", "migrations") }

func (h *helper) currentVersion() string {
	data, err := os.ReadFile(filepath.Join(h.root, "VERSION"))
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return "local"
	}
	return strings.TrimSpace(string(data))
}

func (h *helper) beginLocked(kind string, phase hostupdate.MigrationPhase, message string) {
	now := time.Now().UTC()
	h.operation = hostupdate.MigrationOperation{ID: randomID(), Kind: kind, Phase: phase, StartedAt: &now, Logs: []hostupdate.MigrationLog{{At: now, Phase: phase, Message: message}}}
}

func (h *helper) setPhase(phase hostupdate.MigrationPhase, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.operation.Phase = phase
	h.appendLogLocked(phase, message)
}

func (h *helper) fail(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now().UTC()
	h.operation.Phase = hostupdate.MigrationPhaseFailed
	h.operation.Error = operationError(err)
	h.operation.FinishedAt = &now
	h.appendLogLocked(hostupdate.MigrationPhaseFailed, "迁移操作失败")
}

func (h *helper) succeedLocked(archive hostupdate.MigrationArchive, message string) {
	now := time.Now().UTC()
	h.operation.Phase = hostupdate.MigrationPhaseSucceeded
	h.operation.Error = ""
	h.operation.FinishedAt = &now
	h.operation.Archive = &archive
	h.appendLogLocked(hostupdate.MigrationPhaseSucceeded, message)
}

func (h *helper) appendLogLocked(phase hostupdate.MigrationPhase, message string) {
	h.operation.Logs = append(h.operation.Logs, hostupdate.MigrationLog{At: time.Now().UTC(), Phase: phase, Message: message})
	if len(h.operation.Logs) > 200 {
		h.operation.Logs = append([]hostupdate.MigrationLog(nil), h.operation.Logs[len(h.operation.Logs)-200:]...)
	}
}

func (h *helper) snapshot() hostupdate.Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshotLocked()
}

func (h *helper) snapshotLocked() hostupdate.Status {
	operation := h.operation
	operation.Logs = append([]hostupdate.MigrationLog(nil), operation.Logs...)
	if operation.Archive != nil {
		archive := *operation.Archive
		archive.Path = ""
		operation.Archive = &archive
	}
	var last *hostupdate.MigrationArchive
	if h.last != nil {
		archive := *h.last
		archive.Path = ""
		last = &archive
	}
	return hostupdate.Status{
		Supported:      false,
		Connected:      true,
		Repository:     "ddcat-ai/open-ai-canvas",
		Deployment:     "local-migration-helper",
		CurrentVersion: h.currentVersion(),
		Checks:         []hostupdate.Check{{Key: "migration-helper", Label: "本地迁移助手", Status: "passed", Detail: runtime.GOOS + "/" + runtime.GOARCH + " 本机固定迁移脚本", Blocking: false}},
		Operation:      hostupdate.Operation{Phase: hostupdate.PhaseIdle, Logs: []hostupdate.LogEntry{}},
		Migration:      hostupdate.MigrationStatus{Supported: true, MaxArchiveSize: h.maxBytes, LastExport: last, Operation: operation},
	}
}

func (h *helper) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (h *helper) writeError(w http.ResponseWriter, status int, err error, data any) {
	h.writeJSON(w, status, map[string]any{"error": err.Error(), "data": data})
}

func validateLocalMigrationArchive(archivePath string, limit int64) error {
	info, err := os.Stat(archivePath)
	if err != nil {
		return err
	}
	if info.Size() <= 0 || info.Size() > limit {
		return errors.New("迁移包大小无效")
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开迁移包：%w", err)
	}
	defer archive.Close()
	if len(archive.File) == 0 || len(archive.File) > 2048 {
		return errors.New("迁移包文件数量无效")
	}
	files := make(map[string]*zip.File, len(archive.File))
	var expanded uint64
	for _, entry := range archive.File {
		if entry == nil || !safeArchiveName(entry.Name) || entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("迁移包包含不安全路径：%s", entry.Name)
		}
		if strings.HasSuffix(entry.Name, "/") {
			continue
		}
		if _, exists := files[entry.Name]; exists {
			return fmt.Errorf("迁移包包含重复文件 %s", entry.Name)
		}
		if entry.UncompressedSize64 > uint64(limit) || expanded > uint64(limit)*4-entry.UncompressedSize64 {
			return errors.New("迁移包解压后体积异常")
		}
		expanded += entry.UncompressedSize64
		files[entry.Name] = entry
	}
	manifestEntry := files["manifest.json"]
	if manifestEntry == nil {
		return errors.New("迁移包缺少 manifest.json")
	}
	manifestData, err := readZipEntry(manifestEntry, 2<<20)
	if err != nil {
		return err
	}
	var manifest localMigrationManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("解析迁移包清单：%w", err)
	}
	if manifest.SchemaVersion != 1 || strings.TrimSpace(manifest.Version) == "" || manifest.DatabaseDriver != "sqlite" || len(manifest.Files) == 0 {
		return errors.New("迁移包格式不受支持")
	}
	listed := make(map[string]localMigrationFile, len(manifest.Files))
	for _, item := range manifest.Files {
		if !safeArchiveName(item.Path) || item.Path == "manifest.json" || item.Size < 0 || item.Size > limit || len(item.SHA256) != 64 {
			return errors.New("迁移包清单包含无效文件")
		}
		if _, exists := listed[item.Path]; exists {
			return fmt.Errorf("迁移包清单包含重复文件 %s", item.Path)
		}
		entry := files[item.Path]
		if entry == nil || int64(entry.UncompressedSize64) != item.Size {
			return fmt.Errorf("迁移包文件缺失或大小不匹配：%s", item.Path)
		}
		actual, err := hashZipEntry(entry)
		if err != nil {
			return err
		}
		if !strings.EqualFold(actual, item.SHA256) {
			return fmt.Errorf("迁移包 SHA-256 校验失败：%s", item.Path)
		}
		listed[item.Path] = item
	}
	if _, ok := listed["images.tar"]; !ok {
		return errors.New("迁移包缺少 Docker 镜像归档")
	}
	if _, ok := listed["service-config/.env"]; !ok {
		return errors.New("迁移包缺少本地环境配置")
	}
	for name := range files {
		if name != "manifest.json" {
			if _, exists := listed[name]; !exists {
				return fmt.Errorf("迁移包包含未列入清单的文件 %s", name)
			}
		}
	}
	return nil
}

func safeArchiveName(name string) bool {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return false
	}
	if strings.HasSuffix(name, "/") {
		name = strings.TrimSuffix(name, "/")
	}
	cleaned := path.Clean(name)
	return cleaned == name && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func readZipEntry(entry *zip.File, limit int64) ([]byte, error) {
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
		return nil, errors.New("迁移包文件超过限制")
	}
	return data, nil
}

func hashZipEntry(entry *zip.File) (string, error) {
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

func fileChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func safeDownloadName(value string) string {
	if value == "" {
		return "open-ai-canvas-migration"
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return "open-ai-canvas-migration"
		}
	}
	return value
}

func randomID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

func operationError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 1200 {
		return message[:1200]
	}
	return message
}
