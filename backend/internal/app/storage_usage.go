package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/platform"
	"infinite-canvas/backend/internal/repository"

	"golang.org/x/sync/errgroup"
)

const storageMeasurementTimeout = 30 * time.Second

type storedObjectStat struct {
	Bytes     int64
	Exists    bool
	CheckedAt time.Time
}

type accountStorageMeasurement struct {
	Usage    AccountFileStorageUsage
	Revision string
}

type storageMeasurementObject struct {
	resource *model.Resource
	root     string
	path     string
	pending  bool
	session  bool
}

func storageSnapshotRevision(snapshot repository.FileStorageSnapshot) (string, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("无法核实文件记录版本：%w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) measureAccountStorage(ctx context.Context, userID string, refresh bool) (accountStorageMeasurement, error) {
	ctx, cancel := context.WithTimeout(ctx, storageMeasurementTimeout)
	defer cancel()
	s.storageUsageOnce.Do(func() {
		s.storageObjectStats = platform.NewBoundedReadCache[string, storedObjectStat](4096, 4<<20, 32, 30*time.Second)
		s.storageMeasurementSlots = make(chan struct{}, 4)
	})
	select {
	case s.storageMeasurementSlots <- struct{}{}:
		defer func() { <-s.storageMeasurementSlots }()
	case <-ctx.Done():
		return accountStorageMeasurement{}, ctx.Err()
	}
	snapshot, err := s.repo.UserFileStorageSnapshot(ctx, userID)
	if err != nil {
		return accountStorageMeasurement{}, err
	}
	revision, err := storageSnapshotRevision(snapshot)
	if err != nil {
		return accountStorageMeasurement{}, err
	}
	result := accountStorageMeasurement{Revision: revision}
	objects, err := s.storageMeasurementObjects(snapshot)
	if err != nil {
		return result, err
	}
	stats := make([]storedObjectStat, len(objects))
	group, measurementCtx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for index, object := range objects {
		group.Go(func() error {
			ctx := measurementCtx
			if err := ctx.Err(); err != nil {
				return err
			}
			var stat storedObjectStat
			var err error
			if object.resource == nil {
				stat, err = statStoredLocalFile(object.root, object.path)
			} else {
				resource := object.resource
				load := func(ctx context.Context) (storedObjectStat, int, error) {
					setting, err := s.ossSettingForResource(userID, resource)
					if err != nil {
						return storedObjectStat{}, 0, err
					}
					stat, err := statStoredCloudObject(ctx, setting, resource.ObjectKey)
					if ctx.Err() != nil {
						return storedObjectStat{}, 0, ctx.Err()
					}
					return stat, 256, err
				}
				key := fmt.Sprintf("%s:%s:%s:%s:%d:%d:%s", userID, resourceStorageIdentity(resource),
					resource.ID, resource.StorageSettingID, resource.Size, resource.UpdatedAt.UnixNano(), resource.ETag)
				// Writes and explicit recounts never reuse a read projection (including
				// an in-flight pre-refresh query). Outbox files must observe deletion.
				if refresh || object.pending {
					s.storageObjectStats.Invalidate(key)
					stat, _, err = load(ctx)
				} else {
					stat, err = s.storageObjectStats.Get(ctx, key, load)
				}
			}
			if err != nil {
				return fmt.Errorf("无法核实账号文件容量：%w", err)
			}
			stats[index] = stat
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Usage.CheckedAt = time.Now().UTC()
	for index, stat := range stats {
		if stat.Bytes < 0 || stat.Bytes > math.MaxInt64-result.Usage.UsedBytes {
			return result, errors.New("文件容量统计超出有效范围")
		}
		result.Usage.UsedBytes += stat.Bytes
		if stat.CheckedAt.Before(result.Usage.CheckedAt) {
			result.Usage.CheckedAt = stat.CheckedAt
		}
		switch {
		case objects[index].session:
			result.Usage.SessionBytes += stat.Bytes
		case objects[index].pending:
			result.Usage.PendingDeletionBytes += stat.Bytes
		default:
			result.Usage.ResourceBytes += stat.Bytes
		}
	}
	return result, nil
}

func (s *Service) storageMeasurementObjects(snapshot repository.FileStorageSnapshot) ([]storageMeasurementObject, error) {
	objects := make([]storageMeasurementObject, 0, len(snapshot.Resources)+len(snapshot.Sessions)+len(snapshot.Deletions))
	seen := make(map[string]struct{})
	addResource := func(resource model.Resource, pending bool) error {
		resource.Provider = normalizedResourceProvider(resource.Provider)
		resource.Endpoint = strings.TrimRight(strings.TrimSpace(resource.Endpoint), "/")
		resource.Bucket = strings.TrimSpace(resource.Bucket)
		if strings.TrimSpace(resource.ObjectKey) == "" {
			return errors.New("资源缺少存储路径，无法核实账号容量")
		}
		object := storageMeasurementObject{resource: &resource, pending: pending}
		key := resourceStorageIdentity(&resource)
		if resource.Provider == "local" {
			object.root, object.path = filepath.Join(s.dataDir, "resources"), filepath.FromSlash(resource.ObjectKey)
			object.resource = nil
			key = "resource-local:" + filepath.Clean(object.path)
		}
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			objects = append(objects, object)
		}
		return nil
	}
	for _, resource := range snapshot.Resources {
		if err := addResource(resource, false); err != nil {
			return nil, err
		}
	}
	for _, job := range snapshot.Deletions {
		if err := addResource(model.Resource{ID: job.ResourceID, UserID: job.UserID, Provider: job.Provider,
			Endpoint: job.Endpoint, Bucket: job.Bucket, ObjectKey: job.ObjectKey, StorageSettingID: job.StorageSettingID}, true); err != nil {
			return nil, err
		}
	}
	uploadRoot, err := filepath.Abs(filepath.Join(s.dataDir, "uploads"))
	if err != nil {
		return nil, err
	}
	for _, file := range snapshot.Sessions {
		if strings.TrimSpace(file.Path) == "" {
			return nil, errors.New("会话附件缺少存储路径，无法核实账号容量")
		}
		fullPath, err := filepath.Abs(file.Path)
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(uploadRoot, fullPath)
		if err != nil || !filepath.IsLocal(relative) || relative == "." {
			return nil, errors.New("会话附件路径超出账号文件存储目录")
		}
		key := "session-local:" + filepath.Clean(relative)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			objects = append(objects, storageMeasurementObject{root: uploadRoot, path: relative, session: true})
		}
	}
	return objects, nil
}

func statStoredLocalFile(rootPath, path string) (storedObjectStat, error) {
	if !filepath.IsLocal(path) || filepath.Clean(path) == "." {
		return storedObjectStat{}, errors.New("文件路径超出允许的存储目录")
	}
	stat := storedObjectStat{CheckedAt: time.Now().UTC()}
	root, err := os.OpenRoot(rootPath)
	if errors.Is(err, os.ErrNotExist) {
		return stat, nil
	}
	if err != nil {
		return stat, errors.New("无法访问账号文件存储目录")
	}
	defer root.Close()
	info, err := root.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return stat, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return stat, errors.New("无法核实本地文件，文件不可访问或路径不是普通文件")
	}
	stat.Bytes, stat.Exists = info.Size(), true
	return stat, nil
}
