package hostupdate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxReleaseResponseBytes = 4 << 20
	defaultArchiveMaxBytes  = 20 << 30
)

type Config struct {
	Repository         string
	InstallDir         string
	ComposeFile        string
	ReleaseComposeFile string
	EnvFile            string
	StateDir           string
	BackupDir          string
	HealthURL          string
	GitHubToken        string
	StableWindow       time.Duration
	StepTimeout        time.Duration
	BinaryPath         string
	ServiceName        string
	SelfUpdate         bool
	MigrationMaxBytes  int64
	ManagedCompose     bool
	ContainerMode      bool
}

type commandRunner interface {
	Run(context.Context, string, []string, []string, io.Writer, io.Writer) error
}

type commandInputRunner interface {
	RunWithInput(context.Context, string, []string, []string, io.Reader, io.Writer, io.Writer) error
}

type execRunner struct{ dir string }

func (r execRunner) Run(ctx context.Context, name string, args, environment []string, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = r.dir
	command.Env = append(os.Environ(), environment...)
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func (r execRunner) RunWithInput(ctx context.Context, name string, args, environment []string, input io.Reader, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = r.dir
	command.Env = append(os.Environ(), environment...)
	command.Stdin = input
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

type persistedState struct {
	LatestRelease       *Release           `json:"latestRelease,omitempty"`
	LastBackup          *Backup            `json:"lastBackup,omitempty"`
	LastMigrationExport *MigrationArchive  `json:"lastMigrationExport,omitempty"`
	RollbackVersion     string             `json:"rollbackVersion,omitempty"`
	Operation           Operation          `json:"operation"`
	Migration           MigrationOperation `json:"migration"`
}

type Manager struct {
	mu         sync.Mutex
	config     Config
	runner     commandRunner
	httpClient *http.Client
	state      persistedState
}

func NewManager(config Config) (*Manager, error) {
	return newManager(config, false)
}

// NewConfigurator only reads operation state. Installing the helper must never
// mark a running update interrupted or overwrite its recovery information.
func NewConfigurator(config Config) (*Manager, error) {
	return newManager(config, true)
}

func newManager(config Config, configurationOnly bool) (*Manager, error) {
	config.Repository = strings.TrimSpace(config.Repository)
	if config.Repository == "" {
		config.Repository = "buttonslaybaugh397-art/open-ai-canvas"
	}
	if config.InstallDir == "" {
		config.InstallDir = "/opt/open-ai-canvas"
	}
	if config.ComposeFile == "" {
		config.ComposeFile = "docker-compose.deploy.yml"
	}
	if config.ReleaseComposeFile == "" {
		config.ReleaseComposeFile = config.ComposeFile
	}
	if filepath.Base(config.ReleaseComposeFile) != config.ReleaseComposeFile || strings.ContainsAny(config.ReleaseComposeFile, `/\?#`) || (filepath.Ext(config.ReleaseComposeFile) != ".yml" && filepath.Ext(config.ReleaseComposeFile) != ".yaml") {
		return nil, errors.New("Release Compose 必须是仓库根目录中的 YAML 文件名")
	}
	if config.EnvFile == "" {
		config.EnvFile = ".env"
	}
	if config.StateDir == "" {
		config.StateDir = "/var/lib/open-ai-canvas-updater"
	}
	if config.BackupDir == "" {
		config.BackupDir = filepath.Join(config.InstallDir, "backups")
	}
	if config.StableWindow <= 0 {
		config.StableWindow = 30 * time.Second
	}
	if config.StepTimeout <= 0 {
		config.StepTimeout = 20 * time.Minute
	}
	if config.MigrationMaxBytes <= 0 {
		config.MigrationMaxBytes = defaultArchiveMaxBytes
	}
	if configurationOnly {
		manager := &Manager{config: config, runner: execRunner{dir: config.InstallDir}}
		if err := manager.loadState(); err != nil {
			return nil, err
		}
		if manager.state.Operation.Phase.Active() || manager.state.Migration.Phase.Active() || manager.state.Operation.Phase == PhaseManualIntervention || manager.state.Migration.Phase == MigrationPhaseManualAction {
			return nil, errors.New("已有更新或迁移正在进行，或存在未处理的恢复状态，拒绝重新安装更新器")
		}
		return manager, nil
	}
	if err := os.MkdirAll(config.StateDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建更新器状态目录：%w", err)
	}
	if err := os.MkdirAll(config.BackupDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建备份目录：%w", err)
	}
	manager := &Manager{
		config:     config,
		runner:     execRunner{dir: config.InstallDir},
		httpClient: &http.Client{Timeout: 30 * time.Second},
		state:      persistedState{Operation: Operation{Phase: PhaseIdle, Logs: []LogEntry{}}, Migration: MigrationOperation{Phase: MigrationPhaseIdle, Logs: []MigrationLog{}}},
	}
	if err := manager.loadState(); err != nil {
		return nil, err
	}
	if manager.state.Operation.Phase.Active() {
		manager.state.Operation.Phase = PhaseManualIntervention
		manager.state.Operation.Error = "更新器进程在操作期间退出，请检查容器与数据库状态后手动处理"
		manager.appendLogLocked(PhaseManualIntervention, manager.state.Operation.Error)
		_ = manager.saveStateLocked()
	}
	if manager.state.Migration.Phase.Active() {
		manager.state.Migration.Phase = MigrationPhaseManualAction
		manager.state.Migration.Error = "更新器进程在迁移期间退出，请检查容器与数据状态后手动处理"
		manager.appendMigrationLogLocked(MigrationPhaseManualAction, manager.state.Migration.Error)
		_ = manager.saveStateLocked()
	}
	if config.ManagedCompose && manager.state.Operation.Phase != PhaseManualIntervention && manager.state.Migration.Phase != MigrationPhaseManualAction {
		if err := manager.syncComposeConfig(context.Background()); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

func (m *Manager) Snapshot() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Manager) migrationLimit() int64 {
	return m.config.MigrationMaxBytes
}

func (m *Manager) Check(ctx context.Context) (Status, error) {
	m.mu.Lock()
	if m.state.Operation.Phase.Active() || m.state.Migration.Phase.Active() {
		status := m.snapshotLocked()
		m.mu.Unlock()
		return status, errors.New("更新操作正在进行，暂时不能检查新版本")
	}
	if err := m.syncComposeConfig(ctx); err != nil {
		status := m.snapshotLocked()
		m.mu.Unlock()
		return status, err
	}
	m.state.Operation.Phase = PhaseChecking
	m.state.Operation.Error = ""
	m.appendLogLocked(PhaseChecking, "正在读取 GitHub Release")
	_ = m.saveStateLocked()
	m.mu.Unlock()

	release, err := m.latestRelease(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.state.Operation.Phase = PhaseFailed
		m.state.Operation.Error = err.Error()
		m.appendLogLocked(PhaseFailed, "检查更新失败")
		_ = m.saveStateLocked()
		return m.snapshotLocked(), err
	}
	m.state.LatestRelease = release
	current, currentErr := m.currentVersion()
	if currentErr != nil {
		m.state.Operation.Phase = PhaseFailed
		m.state.Operation.Error = currentErr.Error()
		_ = m.saveStateLocked()
		return m.snapshotLocked(), currentErr
	}
	if CompareVersions(current, release.Version) < 0 {
		m.state.Operation.Phase = PhaseReady
		m.appendLogLocked(PhaseReady, fmt.Sprintf("发现新版本 %s", release.Version))
	} else {
		m.state.Operation.Phase = PhaseNoUpdate
		m.appendLogLocked(PhaseNoUpdate, "当前已是最新版本")
	}
	_ = m.saveStateLocked()
	return m.snapshotLocked(), nil
}

func (m *Manager) StartUpdate(targetVersion string) (Status, error) {
	targetVersion = strings.TrimSpace(targetVersion)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Operation.Phase.Active() {
		return m.snapshotLocked(), errors.New("已有更新操作正在进行")
	}
	if m.state.Migration.Phase.Active() {
		return m.snapshotLocked(), errors.New("数据迁移正在进行，暂时不能开始更新")
	}
	if m.state.LatestRelease == nil || m.state.LatestRelease.Version != targetVersion {
		return m.snapshotLocked(), errors.New("目标版本与最近一次检查结果不一致，请重新检查更新")
	}
	if err := m.syncComposeConfig(context.Background()); err != nil {
		return m.snapshotLocked(), err
	}
	current, err := m.currentVersion()
	if err != nil {
		return m.snapshotLocked(), err
	}
	if CompareVersions(current, targetVersion) >= 0 {
		return m.snapshotLocked(), errors.New("目标版本必须高于当前版本")
	}
	now := time.Now().UTC()
	m.state.Operation = Operation{
		ID:            randomID(),
		Phase:         PhasePreflight,
		FromVersion:   current,
		TargetVersion: targetVersion,
		StartedAt:     &now,
		Logs:          []LogEntry{{At: now, Phase: PhasePreflight, Message: "开始更新前检查"}},
	}
	_ = m.saveStateLocked()
	go m.runUpdate(current, targetVersion)
	return m.snapshotLocked(), nil
}

func (m *Manager) StartRollback(reason string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Operation.Phase.Active() {
		return m.snapshotLocked(), errors.New("已有更新操作正在进行")
	}
	if m.state.Migration.Phase.Active() {
		return m.snapshotLocked(), errors.New("数据迁移正在进行，暂时不能开始回退")
	}
	if m.state.RollbackVersion == "" || m.state.LastBackup == nil {
		return m.snapshotLocked(), errors.New("没有可用的回退版本或已校验备份")
	}
	if err := m.syncComposeConfig(context.Background()); err != nil {
		return m.snapshotLocked(), err
	}
	current, err := m.currentVersion()
	if err != nil {
		return m.snapshotLocked(), err
	}
	now := time.Now().UTC()
	target := m.state.RollbackVersion
	m.state.Operation = Operation{
		ID:            randomID(),
		Phase:         PhaseRollingBack,
		FromVersion:   current,
		TargetVersion: target,
		StartedAt:     &now,
		Logs:          []LogEntry{{At: now, Phase: PhaseRollingBack, Message: "管理员发起人工回退：" + strings.TrimSpace(reason)}},
	}
	_ = m.saveStateLocked()
	backup := *m.state.LastBackup
	go m.runRollback(target, backup, false)
	return m.snapshotLocked(), nil
}

func (m *Manager) runUpdate(fromVersion, targetVersion string) {
	nextCompose, err := m.prepareTargetCompose(targetVersion)
	if err != nil {
		m.failWithoutRollback(PhaseFailed, err)
		return
	}
	defer os.Remove(nextCompose)
	if err := m.preflight(nextCompose, targetVersion); err != nil {
		m.failWithoutRollback(PhaseFailed, err)
		return
	}
	updaterBinary, err := m.prepareUpdaterBinary(targetVersion)
	if err != nil {
		m.failWithoutRollback(PhaseFailed, err)
		return
	}
	if updaterBinary != "" {
		defer os.Remove(updaterBinary)
	}

	if err := replaceFile(m.composePath(), m.previousComposePath(), 0o600); err != nil {
		m.failWithoutRollback(PhaseFailed, fmt.Errorf("保存旧 Compose 配置：%w", err))
		return
	}

	m.setPhase(PhasePulling, "拉取目标版本镜像")
	if err := m.compose(nextCompose, targetVersion, m.config.StepTimeout, nil, "pull", "backend", "web"); err != nil {
		m.failWithoutRollback(PhaseFailed, err)
		return
	}
	if err := m.verifyImages(targetVersion); err != nil {
		m.failWithoutRollback(PhaseFailed, err)
		return
	}

	m.setPhase(PhaseDraining, "停止 Web 与 Backend，等待后台任务优雅退出")
	if err := m.compose(m.composePath(), fromVersion, m.config.StepTimeout, nil, "stop", "web", "backend"); err != nil {
		m.failAfterStop(err, fromVersion)
		return
	}

	m.setPhase(PhaseBackingUp, "创建数据库、文件、缓存与部署配置恢复包")
	backup, err := m.createBackup(fromVersion)
	if err != nil {
		m.failAfterStop(err, fromVersion)
		return
	}
	m.mu.Lock()
	m.state.LastBackup = &backup
	m.state.RollbackVersion = fromVersion
	_ = m.saveStateLocked()
	m.mu.Unlock()
	m.setPhase(PhaseMigrating, "执行目标版本数据库迁移")
	if err := m.compose(nextCompose, targetVersion, m.config.StepTimeout, nil, "run", "--rm", "migrate"); err != nil {
		m.failWithRollback(err, fromVersion, backup)
		return
	}

	m.setPhase(PhaseSwitching, "切换 Compose 配置与镜像版本")
	if err := setEnvValue(m.envPath(), "CANVAS_IMAGE_TAG", strings.TrimPrefix(targetVersion, "v")); err != nil {
		m.failWithRollback(err, fromVersion, backup)
		return
	}
	if err := replaceFile(nextCompose, m.composePath(), 0o600); err != nil {
		_ = setEnvValue(m.envPath(), "CANVAS_IMAGE_TAG", strings.TrimPrefix(fromVersion, "v"))
		m.failWithRollback(err, fromVersion, backup)
		return
	}
	if err := m.startApplicationServices(targetVersion); err != nil {
		m.failWithRollback(err, fromVersion, backup)
		return
	}

	m.setPhase(PhaseVerifying, "验证健康状态、运行版本和稳定窗口")
	if err := m.verifyHealthy(targetVersion); err != nil {
		m.failWithRollback(err, fromVersion, backup)
		return
	}
	if updaterBinary != "" {
		if err := replaceFile(updaterBinary, m.config.BinaryPath, 0o755); err != nil {
			m.mu.Lock()
			now := time.Now().UTC()
			m.state.Operation.Phase = PhaseManualIntervention
			m.state.Operation.FinishedAt = &now
			m.state.Operation.Error = "应用已更新，但 Host Updater 自更新失败：" + safeOperationError(err)
			m.appendLogLocked(PhaseManualIntervention, "应用版本已切换，更新器二进制需要人工更新")
			_ = m.saveStateLocked()
			m.mu.Unlock()
			return
		}
	}
	m.mu.Lock()
	now := time.Now().UTC()
	m.state.Operation.Phase = PhaseSucceeded
	m.state.Operation.FinishedAt = &now
	m.state.Operation.Error = ""
	m.appendLogLocked(PhaseSucceeded, fmt.Sprintf("已成功更新到 %s", targetVersion))
	if updaterBinary != "" {
		m.appendLogLocked(PhaseSucceeded, "Host Updater 二进制已同步到目标 Release")
	}
	_ = m.saveStateLocked()
	m.mu.Unlock()
	if updaterBinary != "" && m.config.ServiceName != "" {
		go m.restartSelf()
	}
}

func (m *Manager) failWithoutRollback(phase Phase, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	m.state.Operation.Phase = phase
	m.state.Operation.Error = safeOperationError(err)
	m.state.Operation.FinishedAt = &now
	m.appendLogLocked(phase, "更新已中止，尚未修改运行版本")
	_ = m.saveStateLocked()
}

func (m *Manager) failAfterStop(cause error, version string) {
	recoveryErr := m.startRollbackServices(version)
	if recoveryErr == nil {
		recoveryErr = m.verifyHealthy(version)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	m.state.Operation.FinishedAt = &now
	m.state.Operation.Error = safeOperationError(cause)
	if recoveryErr == nil {
		m.state.Operation.Phase = PhaseFailed
		m.state.Operation.RollbackError = ""
		m.appendLogLocked(PhaseFailed, "备份失败，已恢复并验证更新前服务")
	} else {
		m.state.Operation.Phase = PhaseManualIntervention
		m.state.Operation.RollbackError = safeOperationError(recoveryErr)
		m.appendLogLocked(PhaseManualIntervention, "备份失败且更新前服务恢复失败，需要人工介入")
	}
	_ = m.saveStateLocked()
}

func (m *Manager) failWithRollback(cause error, targetVersion string, backup Backup) {
	m.mu.Lock()
	m.state.Operation.Error = safeOperationError(cause)
	m.state.Operation.AutomaticRollback = true
	m.state.Operation.Phase = PhaseRollingBack
	m.appendLogLocked(PhaseRollingBack, "更新失败，正在自动恢复旧版本和完整数据备份")
	_ = m.saveStateLocked()
	m.mu.Unlock()
	m.runRollback(targetVersion, backup, true)
}

func (m *Manager) runRollback(targetVersion string, backup Backup, automatic bool) {
	var failures []error
	if strings.TrimSpace(backup.Path) == "" {
		failures = append(failures, errors.New("恢复包路径为空"))
	} else if err := verifyZipBackup(backup.Path, backup.Checksum, backupArchiveLimit(m.config.MigrationMaxBytes)); err != nil {
		failures = append(failures, fmt.Errorf("恢复包校验失败：%w", err))
	}
	if len(failures) == 0 {
		if err := m.compose(m.composePath(), targetVersion, 5*time.Minute, nil, "stop", "web", "backend"); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) == 0 && backup.Path != "" {
		if _, err := restoreBackupServiceFile(backup.Path, "service/.env", m.envPath()); err != nil {
			failures = append(failures, fmt.Errorf("恢复部署环境：%w", err))
		}
	}
	if len(failures) == 0 && backup.Path != "" {
		restored, err := restoreBackupServiceFile(backup.Path, "service/compose.yml", m.composePath())
		if err != nil {
			failures = append(failures, fmt.Errorf("恢复 Compose 配置：%w", err))
		} else if !restored {
			// Backups created before format 2 use the updater's Compose snapshot.
			if previousErr := replaceFile(m.previousComposePath(), m.composePath(), 0o600); previousErr != nil {
				failures = append(failures, fmt.Errorf("恢复旧 Compose 配置：%w", previousErr))
			}
		}
	}
	if len(failures) == 0 && backup.Path != "" {
		if err := m.compose(m.composePath(), targetVersion, 2*time.Minute, nil, "config", "--quiet"); err != nil {
			failures = append(failures, fmt.Errorf("恢复后的 Compose 配置无效：%w", err))
		}
	}
	if len(failures) == 0 && backup.Path != "" {
		if err := m.restoreDeploymentSecrets(backup); err != nil {
			failures = append(failures, fmt.Errorf("恢复部署密钥卷：%w", err))
		}
	}
	if len(failures) == 0 && backup.Path != "" {
		if err := m.restoreDatabase(backup); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) == 0 && backup.Path != "" {
		if err := m.restoreBackendData(backup); err != nil {
			failures = append(failures, fmt.Errorf("恢复后端数据目录：%w", err))
		}
	}
	if len(failures) == 0 && backup.Path != "" {
		if err := m.restoreRedisData(backup); err != nil {
			failures = append(failures, fmt.Errorf("恢复 Redis 数据目录：%w", err))
		}
	}
	if len(failures) == 0 {
		if err := setEnvValue(m.envPath(), "CANVAS_IMAGE_TAG", strings.TrimPrefix(targetVersion, "v")); err != nil {
			failures = append(failures, err)
		}
	}
	started := false
	if len(failures) == 0 {
		started = true
		if err := m.startRollbackServices(targetVersion); err != nil {
			started = false
			failures = append(failures, err)
		}
	}
	if started {
		if err := m.verifyHealthy(targetVersion); err != nil {
			failures = append(failures, err)
		}
	}
	rollbackErr := errors.Join(failures...)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	m.state.Operation.FinishedAt = &now
	m.state.Operation.AutomaticRollback = automatic
	if rollbackErr != nil {
		m.state.Operation.Phase = PhaseManualIntervention
		m.state.Operation.RollbackError = safeOperationError(rollbackErr)
		m.appendLogLocked(PhaseManualIntervention, "自动回退未能完成，需要人工介入")
	} else {
		m.state.Operation.Phase = PhaseRolledBack
		m.state.Operation.RollbackError = ""
		m.appendLogLocked(PhaseRolledBack, fmt.Sprintf("已恢复到 %s", targetVersion))
	}
	_ = m.saveStateLocked()
}

func (m *Manager) startRollbackServices(version string) error {
	primaryErr := m.startApplicationServices(version)
	if primaryErr == nil {
		return nil
	}
	m.setPhase(PhaseRollingBack, "标准启动失败，正在兼容旧版 Compose 健康依赖")
	if err := m.compose(m.composePath(), version, m.config.StepTimeout, nil, "up", "-d", "postgres", "redis"); err != nil {
		return errors.Join(primaryErr, err)
	}
	if err := m.compose(m.composePath(), version, m.config.StepTimeout, nil, "up", "-d", "--no-deps", "backend"); err != nil {
		return errors.Join(primaryErr, err)
	}
	if err := m.compose(m.composePath(), version, m.config.StepTimeout, nil, "up", "-d", "--no-deps", "web"); err != nil {
		return errors.Join(primaryErr, err)
	}
	return nil
}

func (m *Manager) startApplicationServices(version string) error {
	if !m.config.ContainerMode {
		return m.compose(m.composePath(), version, m.config.StepTimeout, nil, "up", "-d", "--remove-orphans", "--wait", "--wait-timeout", "600")
	}
	// Never reconcile the running updater, including indirectly via backend's
	// dependency, while it is responsible for completing a recovery transaction.
	for _, services := range [][]string{{"postgres", "redis"}, {"backend"}, {"web"}} {
		args := append([]string{"up", "-d", "--no-deps", "--wait", "--wait-timeout", "600"}, services...)
		if err := m.compose(m.composePath(), version, m.config.StepTimeout, nil, args...); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) snapshotLocked() Status {
	current, err := m.currentVersion()
	checks := m.checks(current, err)
	status := Status{
		Supported:       true,
		Connected:       true,
		Repository:      m.config.Repository,
		Deployment:      "docker-compose-host-updater",
		CurrentVersion:  current,
		LatestRelease:   cloneRelease(m.state.LatestRelease),
		Checks:          checks,
		LastBackup:      cloneBackup(m.state.LastBackup),
		RollbackVersion: m.state.RollbackVersion,
		Operation:       cloneOperation(m.state.Operation),
		Migration: MigrationStatus{
			Supported:      m.migrationSupported(),
			Reason:         m.migrationSupportReason(),
			MaxArchiveSize: m.config.MigrationMaxBytes,
			LastExport:     cloneMigrationArchive(m.state.LastMigrationExport),
			Operation:      cloneMigrationOperation(m.state.Migration),
		},
	}
	if status.LatestRelease != nil && err == nil {
		status.UpdateAvailable = CompareVersions(current, status.LatestRelease.Version) < 0
	}
	return status
}

func (m *Manager) checks(current string, currentErr error) []Check {
	items := []Check{
		{Key: "updater", Label: "Host Updater", Status: "passed", Detail: runtime.GOOS + "/" + runtime.GOARCH, Blocking: true},
		{Key: "version", Label: "当前版本", Status: "passed", Detail: current, Blocking: true},
		{Key: "backup", Label: "完整恢复包", Status: "pending", Detail: "停写后自动创建并校验数据库、文件、缓存与部署配置", Blocking: true},
		{Key: "images", Label: "目标镜像", Status: "pending", Detail: "拉取后校验仓库摘要", Blocking: true},
		{Key: "migration", Label: "数据库迁移", Status: "pending", Detail: "在旧服务完全停止后执行", Blocking: true},
	}
	if currentErr != nil {
		items[1].Status = "failed"
		items[1].Detail = currentErr.Error()
	}
	if m.state.LastBackup != nil {
		items[2].Status = "passed"
		items[2].Detail = m.state.LastBackup.ID + " · " + m.state.LastBackup.Checksum
	}
	return items
}

func (m *Manager) setPhase(phase Phase, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Operation.Phase = phase
	m.appendLogLocked(phase, message)
	_ = m.saveStateLocked()
}

func (m *Manager) appendLogLocked(phase Phase, message string) {
	m.state.Operation.Logs = append(m.state.Operation.Logs, LogEntry{At: time.Now().UTC(), Phase: phase, Message: message})
	if len(m.state.Operation.Logs) > 200 {
		m.state.Operation.Logs = append([]LogEntry(nil), m.state.Operation.Logs[len(m.state.Operation.Logs)-200:]...)
	}
}

func (m *Manager) loadState() error {
	data, err := os.ReadFile(m.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取更新器状态：%w", err)
	}
	if err := json.Unmarshal(data, &m.state); err != nil {
		return fmt.Errorf("解析更新器状态：%w", err)
	}
	if m.state.Operation.Phase == "" {
		m.state.Operation.Phase = PhaseIdle
	}
	if m.state.Migration.Phase == "" {
		m.state.Migration.Phase = MigrationPhaseIdle
	}
	return nil
}

func (m *Manager) saveStateLocked() error {
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(m.config.StateDir, ".state-*.json")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
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
	return os.Rename(temporaryName, m.statePath())
}

func (m *Manager) statePath() string { return filepath.Join(m.config.StateDir, "state.json") }
func (m *Manager) composePath() string {
	return filepath.Join(m.config.InstallDir, m.config.ComposeFile)
}
func (m *Manager) envPath() string { return filepath.Join(m.config.InstallDir, m.config.EnvFile) }
func (m *Manager) previousComposePath() string {
	return filepath.Join(m.config.StateDir, "previous-compose.yml")
}

func randomID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buffer)
}

func safeOperationError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 1200 {
		message = message[:1200]
	}
	return message
}

func cloneRelease(value *Release) *Release {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneBackup(value *Backup) *Backup {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Path = ""
	return &cloned
}

func cloneOperation(value Operation) Operation {
	value.Logs = append([]LogEntry(nil), value.Logs...)
	return value
}

func cloneMigrationArchive(value *MigrationArchive) *MigrationArchive {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Path = ""
	return &cloned
}

func cloneMigrationOperation(value MigrationOperation) MigrationOperation {
	value.Archive = cloneMigrationArchive(value.Archive)
	value.Logs = append([]MigrationLog(nil), value.Logs...)
	return value
}
