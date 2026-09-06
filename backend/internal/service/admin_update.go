package service

import (
	"context"
	"io"
	"net/http"
	"strings"

	"infinite-canvas/backend/internal/hostupdate"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/updaterclient"
)

type UpdateManager interface {
	Status(context.Context) (hostupdate.Status, error)
	Check(context.Context) (hostupdate.Status, error)
	Start(context.Context, string) (hostupdate.Status, error)
	Rollback(context.Context, string) (hostupdate.Status, error)
	MigrationExport(context.Context) (hostupdate.Status, error)
	MigrationImport(context.Context, int64, io.Reader) (hostupdate.Status, error)
	OpenMigrationExport(context.Context) (hostupdate.MigrationArchive, io.ReadCloser, error)
}

func (s *Service) ConfigureUpdateManager(manager UpdateManager) {
	s.updateManager = manager
}

func (s *Service) AdminUpdateStatus(ctx context.Context, actor *model.User) (hostupdate.Status, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return hostupdate.Status{}, err
	}
	if s.updateManager == nil {
		return unsupportedUpdateStatus("当前部署未安装 Host Updater"), nil
	}
	status, err := s.updateManager.Status(ctx)
	if err != nil {
		return disconnectedUpdateStatus(updaterConnectionDetail(err)), nil
	}
	return status, nil
}

func (s *Service) AdminCheckUpdate(ctx context.Context, actor *model.User) (hostupdate.Status, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return hostupdate.Status{}, err
	}
	if s.updateManager == nil {
		return hostupdate.Status{}, NewAppError(http.StatusServiceUnavailable, "当前部署未安装 Host Updater")
	}
	status, err := s.updateManager.Check(ctx)
	if err != nil {
		return status, WrapAppError(http.StatusBadGateway, "检查更新失败，请查看更新器状态和日志", err)
	}
	return status, nil
}

func (s *Service) AdminStartUpdate(ctx context.Context, actor *model.User, targetVersion string) (hostupdate.Status, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return hostupdate.Status{}, err
	}
	if s.updateManager == nil {
		return hostupdate.Status{}, NewAppError(http.StatusServiceUnavailable, "当前部署未安装 Host Updater")
	}
	targetVersion = strings.TrimSpace(targetVersion)
	if targetVersion == "" {
		return hostupdate.Status{}, NewAppError(http.StatusBadRequest, "目标版本不能为空")
	}
	status, err := s.updateManager.Start(ctx, targetVersion)
	if err != nil {
		return status, WrapAppError(http.StatusConflict, "无法开始更新，请刷新状态后重试", err)
	}
	return status, nil
}

func (s *Service) AdminRollbackUpdate(ctx context.Context, actor *model.User, reason string) (hostupdate.Status, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return hostupdate.Status{}, err
	}
	if s.updateManager == nil {
		return hostupdate.Status{}, NewAppError(http.StatusServiceUnavailable, "当前部署未安装 Host Updater")
	}
	if strings.TrimSpace(reason) == "" {
		return hostupdate.Status{}, NewAppError(http.StatusBadRequest, "请填写回退原因")
	}
	status, err := s.updateManager.Rollback(ctx, reason)
	if err != nil {
		return status, WrapAppError(http.StatusConflict, "无法开始回退，请检查备份和当前状态", err)
	}
	return status, nil
}

func (s *Service) AdminMigrationStatus(ctx context.Context, actor *model.User) (hostupdate.MigrationStatus, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return hostupdate.MigrationStatus{}, err
	}
	if s.updateManager == nil {
		return unsupportedMigrationStatus("当前部署未安装 Host Updater"), nil
	}
	status, err := s.updateManager.Status(ctx)
	if err != nil {
		return disconnectedMigrationStatus(updaterConnectionDetail(err)), nil
	}
	return status.Migration, nil
}

func (s *Service) AdminStartMigrationExport(ctx context.Context, actor *model.User) (hostupdate.Status, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return hostupdate.Status{}, err
	}
	if s.updateManager == nil {
		return hostupdate.Status{}, NewAppError(http.StatusServiceUnavailable, "当前部署未安装 Host Updater")
	}
	status, err := s.updateManager.MigrationExport(ctx)
	if err != nil {
		return status, WrapAppError(http.StatusConflict, "无法开始导出迁移包，请刷新状态后重试", err)
	}
	return status, nil
}

func (s *Service) AdminImportMigration(ctx context.Context, actor *model.User, contentLength int64, source io.Reader) (hostupdate.Status, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return hostupdate.Status{}, err
	}
	if s.updateManager == nil {
		return hostupdate.Status{}, NewAppError(http.StatusServiceUnavailable, "当前部署未安装 Host Updater")
	}
	if contentLength <= 0 {
		return hostupdate.Status{}, NewAppError(http.StatusBadRequest, "迁移包大小无效")
	}
	status, err := s.updateManager.MigrationImport(ctx, contentLength, source)
	if err != nil {
		return status, WrapAppError(http.StatusConflict, "迁移包未能开始恢复，请检查文件和当前状态", err)
	}
	return status, nil
}

func (s *Service) AdminOpenMigrationExport(ctx context.Context, actor *model.User) (hostupdate.MigrationArchive, io.ReadCloser, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return hostupdate.MigrationArchive{}, nil, err
	}
	if s.updateManager == nil {
		return hostupdate.MigrationArchive{}, nil, NewAppError(http.StatusServiceUnavailable, "当前部署未安装 Host Updater")
	}
	archive, stream, err := s.updateManager.OpenMigrationExport(ctx)
	if err != nil {
		return archive, nil, WrapAppError(http.StatusNotFound, "没有可下载的迁移包，请先完成导出", err)
	}
	return archive, stream, nil
}

func unsupportedUpdateStatus(detail string) hostupdate.Status {
	return hostupdate.Status{
		Supported:  false,
		Connected:  false,
		Deployment: "unsupported",
		Checks:     []hostupdate.Check{{Key: "updater", Label: "Host Updater", Status: "failed", Detail: detail, Blocking: true}},
		Operation:  hostupdate.Operation{Phase: hostupdate.PhaseIdle, Logs: []hostupdate.LogEntry{}},
		Migration:  unsupportedMigrationStatus(detail),
	}
}

func disconnectedUpdateStatus(detail string) hostupdate.Status {
	return hostupdate.Status{
		Supported:  true,
		Connected:  false,
		Deployment: "docker-compose-host-updater",
		Checks:     []hostupdate.Check{{Key: "updater", Label: "Host Updater", Status: "failed", Detail: detail, Blocking: true}},
		Operation:  hostupdate.Operation{Phase: hostupdate.PhaseIdle, Logs: []hostupdate.LogEntry{}},
		Migration:  disconnectedMigrationStatus(detail),
	}
}

func updaterConnectionDetail(err error) string {
	const hint = "请检查 systemd 服务、host-updater 容器、Unix Socket 挂载和 Token 配置"
	if err == nil {
		return "Host Updater 当前不可连接；" + hint
	}
	detail := updaterclient.ConnectionDetail(err)
	return "Host Updater 当前不可连接：" + detail + "；" + hint
}

func unsupportedMigrationStatus(detail string) hostupdate.MigrationStatus {
	return hostupdate.MigrationStatus{
		Supported:      false,
		MaxArchiveSize: 0,
		Operation: hostupdate.MigrationOperation{
			Phase: hostupdate.MigrationPhaseIdle,
			Logs:  []hostupdate.MigrationLog{{Phase: hostupdate.MigrationPhaseFailed, Message: detail}},
		},
	}
}

func disconnectedMigrationStatus(detail string) hostupdate.MigrationStatus {
	return hostupdate.MigrationStatus{
		Supported: true,
		Reason:    detail,
		Operation: hostupdate.MigrationOperation{
			Phase: hostupdate.MigrationPhaseIdle,
			Logs:  []hostupdate.MigrationLog{},
		},
	}
}
