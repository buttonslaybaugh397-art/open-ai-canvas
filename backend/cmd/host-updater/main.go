package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"infinite-canvas/backend/internal/hostupdate"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	if len(os.Args) == 2 && os.Args[1] == "capabilities" {
		fmt.Println("compose-config-v1")
		return nil
	}
	configure := len(os.Args) == 2 && os.Args[1] == "configure"
	checkInstall := len(os.Args) == 2 && os.Args[1] == "check-install"
	if len(os.Args) > 1 && !configure && !checkInstall {
		return errors.New("用法：open-ai-canvas-host-updater [configure]")
	}
	socketPath := env("CANVAS_UPDATER_SOCKET", "/run/open-ai-canvas-updater/updater.sock")
	installDir := env("CANVAS_UPDATER_INSTALL_DIR", "/opt/open-ai-canvas")
	token := strings.TrimSpace(os.Getenv("CANVAS_UPDATER_TOKEN"))
	constructor := hostupdate.NewManager
	if configure || checkInstall {
		constructor = hostupdate.NewConfigurator
	}
	manager, err := constructor(hostupdate.Config{
		Repository:         env("CANVAS_UPDATER_REPOSITORY", "buttonslaybaugh397-art/open-ai-canvas"),
		InstallDir:         installDir,
		ComposeFile:        env("CANVAS_UPDATER_COMPOSE_FILE", "docker-compose.deploy.yml"),
		ReleaseComposeFile: strings.TrimSpace(os.Getenv("CANVAS_UPDATER_RELEASE_COMPOSE_FILE")),
		EnvFile:            env("CANVAS_UPDATER_ENV_FILE", ".env"),
		StateDir:           env("CANVAS_UPDATER_STATE_DIR", "/var/lib/open-ai-canvas-updater"),
		BackupDir:          env("CANVAS_UPDATER_BACKUP_DIR", filepath.Join(installDir, "backups")),
		HealthURL:          strings.TrimSpace(os.Getenv("CANVAS_UPDATER_HEALTH_URL")),
		GitHubToken:        strings.TrimSpace(os.Getenv("CANVAS_UPDATER_GITHUB_TOKEN")),
		StableWindow:       envDuration("CANVAS_UPDATER_STABLE_WINDOW", 30*time.Second),
		StepTimeout:        envDuration("CANVAS_UPDATER_STEP_TIMEOUT", 20*time.Minute),
		BinaryPath:         env("CANVAS_UPDATER_BINARY_PATH", "/usr/local/bin/open-ai-canvas-host-updater"),
		ServiceName:        env("CANVAS_UPDATER_SERVICE_NAME", "open-ai-canvas-updater.service"),
		SelfUpdate:         envBool("CANVAS_UPDATER_SELF_UPDATE", true),
		MigrationMaxBytes:  envBytes("CANVAS_UPDATER_MIGRATION_MAX_BYTES", 20<<30),
		ManagedCompose:     !configure && os.Getenv("CANVAS_UPDATER_CONFIG_SOURCE") == "compose",
	})
	if err != nil {
		return err
	}
	if checkInstall {
		return nil
	}
	if configure {
		info, err := manager.ConfigureDeployment(ctx, os.Getenv("CANVAS_UPDATER_IMAGE_TAG"), os.Getenv("CANVAS_UPDATER_SOCKET_DIR"))
		if err != nil {
			return err
		}
		fmt.Printf("SOCKET_DIR=%s\n", info.SocketDir)
		return nil
	}
	server, err := hostupdate.NewServer(manager, token)
	if err != nil {
		return err
	}
	tokenFile := env("CANVAS_UPDATER_TOKEN_FILE", filepath.Join(filepath.Dir(socketPath), "token"))
	if err := persistTokenFile(tokenFile, token); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return err
	}
	if err := removeStaleSocket(socketPath); err != nil {
		return err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socketPath)
	if err := os.Chmod(socketPath, 0o666); err != nil {
		return err
	}
	httpServer := &http.Server{Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()
	log.Printf("host updater listening on unix://%s", socketPath)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("拒绝删除非 Socket 路径 %s", path)
	}
	return os.Remove(path)
}

func persistTokenFile(path, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("CANVAS_UPDATER_TOKEN 不能为空")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("创建 Host Updater Token 目录：%w", err)
	}
	temporary, err := os.CreateTemp(directory, ".token-*")
	if err != nil {
		return fmt.Errorf("创建 Host Updater Token 临时文件：%w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("设置 Host Updater Token 临时文件权限：%w", err)
	}
	if _, err := temporary.WriteString(token + "\n"); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("写入 Host Updater Token：%w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("同步 Host Updater Token：%w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭 Host Updater Token 临时文件：%w", err)
	}
	if err := os.Chmod(temporaryName, 0o444); err != nil {
		return fmt.Errorf("设置 Host Updater Token 权限：%w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("保存 Host Updater Token：%w", err)
	}
	return nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		log.Fatalf("%s 必须是正数时长", key)
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	if value == "true" || value == "1" || value == "yes" {
		return true
	}
	if value == "false" || value == "0" || value == "no" {
		return false
	}
	log.Fatalf("%s 必须是 true 或 false", key)
	return fallback
}

func envBytes(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		log.Fatalf("%s 必须是正整数字节数", key)
	}
	return parsed
}
