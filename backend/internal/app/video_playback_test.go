package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/platform"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newPlaybackTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	t.Setenv("REDIS_URL", "")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Resource{}, &model.SystemSetting{}, &model.UserOSSSetting{}, &model.StorageLocation{}); err != nil {
		t.Fatal(err)
	}
	coordinator, err := platform.NewCoordinator("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db), dataDir: t.TempDir(), coordinator: coordinator}
	t.Cleanup(func() { _ = svc.Close() })
	return svc, db
}

func TestRequestResourcePlaybackOwnershipAndState(t *testing.T) {
	svc, db := newPlaybackTestService(t)
	t.Setenv(renderFfmpegEnv, filepath.Join(t.TempDir(), "missing-ffmpeg"))
	for _, row := range []model.Resource{
		{ID: "foreign", UserID: "other", Kind: "video", Status: model.ResourceStatusReady},
		{ID: "image", UserID: "owner", Kind: "image", Status: model.ResourceStatusReady},
		{ID: "pending", UserID: "owner", Kind: "video", Status: model.ResourceStatusPending},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		id, user string
		status   int
	}{{"foreign", "owner", 404}, {"missing", "owner", 404}, {"image", "owner", 400}, {"pending", "owner", 409}, {"image", "", 401}} {
		_, err := svc.RequestResourcePlayback(context.Background(), tc.user, tc.id)
		var appErr *AppError
		if !errors.As(err, &appErr) || appErr.Status != tc.status {
			t.Fatalf("%s: %v, want %d", tc.id, err, tc.status)
		}
	}
}

func TestRequestResourcePlaybackMissingBinaryAndTerminalReuse(t *testing.T) {
	svc, db := newPlaybackTestService(t)
	t.Setenv(renderFfmpegEnv, filepath.Join(t.TempDir(), "private-path", "missing-ffmpeg"))
	for _, state := range []string{"", model.PlaybackStatusNone, model.PlaybackStatusReady, model.PlaybackStatusProcessing, model.PlaybackStatusFailed} {
		row := model.Resource{ID: "r-" + state, UserID: "owner", Kind: "video", Status: model.ResourceStatusReady, Provider: "s3", PlaybackStatus: state, PlaybackObjectKey: "retained.mp4"}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		for range 2 {
			got, err := svc.RequestResourcePlayback(context.Background(), "owner", row.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := state
			if state == "" || state == model.PlaybackStatusNone {
				want = model.PlaybackStatusFailed
				if !strings.Contains(got.PlaybackError, "FFmpeg") || strings.Contains(got.PlaybackError, "private-path") || got.PlaybackObjectKey != "" {
					t.Fatalf("unsafe/ambiguous missing binary failure: %#v", got)
				}
			}
			if got.PlaybackStatus != want {
				t.Fatalf("state=%q, want=%q", got.PlaybackStatus, want)
			}
		}
	}
}

func TestResourcePlaybackPollingExpiresOnlyOwnedStaleClaim(t *testing.T) {
	svc, db := newPlaybackTestService(t)
	for _, owner := range []string{"owner", "other"} {
		row := model.Resource{ID: owner, UserID: owner, Kind: "video", Status: model.ResourceStatusReady, PlaybackStatus: model.PlaybackStatusProcessing, PlaybackObjectKey: "expired.mp4", UpdatedAt: time.Now().Add(-time.Hour)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.Resource("owner", "owner")
	if err != nil || got.PlaybackStatus != model.PlaybackStatusFailed {
		t.Fatalf("polling did not expire owned claim: %#v %v", got, err)
	}
	other, err := svc.repo.Resource("other")
	if err != nil || other.PlaybackStatus != model.PlaybackStatusProcessing {
		t.Fatalf("polling expired another account's claim: %#v %v", other, err)
	}
}

func playbackTestBinary(t *testing.T) string {
	t.Helper()
	binary, err := renderFfmpegBinary()
	if err == nil {
		binary, err = exec.LookPath(binary)
	}
	if err != nil {
		t.Skip("real FFmpeg unavailable; set CANVAS_FFMPEG_PATH to run media integration")
	}
	return binary
}

func createPlaybackSource(t *testing.T, binary, dst, codec, pixelFormat string) []byte {
	t.Helper()
	cmd := exec.Command(binary, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi",
		"-i", "color=c=red:s=64x48:r=10:d=0.3", "-c:v", codec, "-pix_fmt", pixelFormat, "-an", dst)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate actual video: %v, %s", err, output)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func awaitPlayback(t *testing.T, svc *Service, id string) *model.Resource {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		resource, err := svc.Resource("owner", id)
		if err != nil {
			t.Fatal(err)
		}
		if resource.PlaybackStatus != model.PlaybackStatusProcessing {
			return resource
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("playback did not reach a terminal state")
	return nil
}

func TestPlaybackRealTranscodeH264HEVCAVIAndWebM(t *testing.T) {
	binary := playbackTestBinary(t)
	for _, format := range []struct{ ext, codec, pixels string }{{".mp4", "libx264", "yuv444p"}, {".mov", "libx265", "yuv420p"}, {".avi", "mpeg4", "yuv420p"}, {".webm", "libvpx-vp9", "yuv420p"}} {
		t.Run(format.ext, func(t *testing.T) {
			svc, db := newPlaybackTestService(t)
			t.Setenv(renderFfmpegEnv, binary)
			root := filepath.Join(svc.dataDir, "resources")
			if err := os.MkdirAll(root, 0o750); err != nil {
				t.Fatal(err)
			}
			data := createPlaybackSource(t, binary, filepath.Join(root, "source"+format.ext), format.codec, format.pixels)
			row := model.Resource{ID: "video", UserID: "owner", Kind: "video", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "source" + format.ext, Size: int64(len(data)), PlaybackStatus: model.PlaybackStatusNone}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			first, err := svc.RequestResourcePlayback(context.Background(), "owner", row.ID)
			if err != nil || first.PlaybackStatus != model.PlaybackStatusProcessing {
				t.Fatalf("start = %#v, %v", first, err)
			}
			repeated, err := svc.RequestResourcePlayback(context.Background(), "owner", row.ID)
			if err != nil || repeated.PlaybackObjectKey != first.PlaybackObjectKey {
				t.Fatalf("duplicate created another claim: %#v, %v", repeated, err)
			}
			result := awaitPlayback(t, svc, row.ID)
			if result.PlaybackStatus != model.PlaybackStatusReady {
				t.Fatalf("real transcode failed: %#v", result)
			}
			output := filepath.Join(svc.dataDir, playbackDirName, result.PlaybackObjectKey)
			if probeVideoCodec(output) != videoCodecH264 {
				t.Fatal("output is not H264 MP4")
			}
			entries, err := os.ReadDir(filepath.Join(svc.dataDir, playbackDirName))
			if err != nil || len(entries) != 1 || entries[0].Name() != result.PlaybackObjectKey {
				t.Fatalf("temporary files leaked: %v, %v", entries, err)
			}
			current, err := os.ReadFile(filepath.Join(root, row.ObjectKey))
			if err != nil || string(current) != string(data) {
				t.Fatal("original was modified")
			}
		})
	}
}

func TestPlaybackRealS3SourceAndNoNestedReferences(t *testing.T) {
	binary := playbackTestBinary(t)
	svc, db := newPlaybackTestService(t)
	t.Setenv(renderFfmpegEnv, binary)
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	video := createPlaybackSource(t, binary, filepath.Join(t.TempDir(), "source.avi"), "mpeg4", "yuv420p")
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if r.URL.Path != "/bucket/clip.avi" {
			t.Errorf("unexpected source request: %s", r.URL.Path)
		}
		_, _ = w.Write(video)
	}))
	defer server.Close()
	defer svc.Close()
	setting, _ := json.Marshal(ossSettingValue{Enabled: true, Provider: s3Provider, Endpoint: server.URL, Bucket: "bucket", Region: "us-east-1", AccessKeyID: "test-key", AccessKeySecret: "test-secret"})
	if err := db.Create(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(setting)}).Error; err != nil {
		t.Fatal(err)
	}
	row := model.Resource{ID: "cloud", UserID: "owner", Kind: "video", Status: model.ResourceStatusReady, Provider: s3Provider, Endpoint: server.URL, Bucket: "bucket", ObjectKey: "clip.avi", Size: int64(len(video))}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RequestResourcePlayback(context.Background(), "owner", row.ID); err != nil {
		t.Fatal(err)
	}
	result := awaitPlayback(t, svc, row.ID)
	if result.PlaybackStatus != model.PlaybackStatusReady || reads.Load() != 1 {
		t.Fatalf("cloud playback = %#v; reads=%d", result, reads.Load())
	}
	stream, err := svc.OpenResourcePlaybackRange("owner", row.ID)
	if err != nil {
		t.Fatal(err)
	}
	stream.Body.Close()
	if stream.Resource.Provider != "local" || stream.Resource.MimeType != "video/mp4" {
		t.Fatalf("cloud playback not local MP4: %#v", stream)
	}
	for _, playlist := range []string{
		"#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n" + server.URL + "/bucket/clip.avi\n#EXT-X-ENDLIST\n",
		"ffconcat version 1.0\nfile '" + filepath.ToSlash(filepath.Join(t.TempDir(), "secret")) + "'\n",
	} {
		work := t.TempDir()
		input := filepath.Join(work, "input")
		if err := os.WriteFile(input, []byte(playlist), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := runH264Transcode(context.Background(), binary, input, filepath.Join(work, "output.mp4")); err == nil {
			t.Fatal("playlist container unexpectedly accepted")
		}
	}
	if reads.Load() != 1 {
		t.Fatal("FFmpeg fetched a nested network reference")
	}
}

func TestPlaybackFailureCleanupCapacityAndCancellation(t *testing.T) {
	binary := playbackTestBinary(t)
	svc, db := newPlaybackTestService(t)
	t.Setenv(renderFfmpegEnv, binary)
	root := filepath.Join(svc.dataDir, "resources")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken"), []byte("not a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	row := model.Resource{ID: "broken", UserID: "owner", Kind: "video", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "broken"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	playbackDir := filepath.Join(svc.dataDir, playbackDirName)
	if err := os.MkdirAll(playbackDir, 0o750); err != nil {
		t.Fatal(err)
	}
	createPlaybackSource(t, binary, filepath.Join(playbackDir, "broken.mp4"), "libx264", "yuv420p")
	release, acquired, err := svc.coordinator.Acquire(context.Background(), "video-playback", 1, time.Minute)
	if err != nil || !acquired {
		t.Fatal("unable to seed concurrency lease")
	}
	release2, acquired, err := svc.coordinator.Acquire(context.Background(), "video-playback", 2, time.Minute)
	if err != nil || !acquired {
		t.Fatal("unable to seed second concurrency lease")
	}
	if _, err := svc.RequestResourcePlayback(context.Background(), "owner", row.ID); err == nil {
		t.Fatal("capacity limit not enforced")
	}
	release()
	release2()
	if _, err := svc.RequestResourcePlayback(context.Background(), "owner", row.ID); err != nil {
		t.Fatal(err)
	}
	result := awaitPlayback(t, svc, row.ID)
	if result.PlaybackStatus != model.PlaybackStatusFailed || result.PlaybackObjectKey != "" || strings.Contains(result.PlaybackError, svc.dataDir) {
		t.Fatalf("unsafe failed state: %#v", result)
	}
	entries, err := os.ReadDir(filepath.Join(svc.dataDir, playbackDirName))
	if err != nil || len(entries) != 1 || entries[0].Name() != "broken.mp4" {
		t.Fatalf("failed transcode leaked files: %v, %v", entries, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runH264Transcode(ctx, binary, filepath.Join(playbackDir, "broken.mp4"), filepath.Join(root, "output.mp4")); err == nil {
		t.Fatal("canceled execution succeeded")
	}
}

func TestPlaybackShutdownCancelsCloudDownloadAndCleansTemporaryFiles(t *testing.T) {
	binary := playbackTestBinary(t)
	svc, db := newPlaybackTestService(t)
	t.Setenv(renderFfmpegEnv, binary)
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	started := make(chan struct{})
	stopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		_, _ = w.Write([]byte("partial-video"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	defer svc.Close()
	setting, _ := json.Marshal(ossSettingValue{Enabled: true, Provider: s3Provider, Endpoint: server.URL, Bucket: "bucket", Region: "us-east-1", AccessKeyID: "test-key", AccessKeySecret: "test-secret"})
	if err := db.Create(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(setting)}).Error; err != nil {
		t.Fatal(err)
	}
	row := model.Resource{ID: "slow", UserID: "owner", Kind: "video", Status: model.ResourceStatusReady, Provider: s3Provider, Endpoint: server.URL, Bucket: "bucket", ObjectKey: "clip.mp4", Size: 1048576}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RequestResourcePlayback(context.Background(), "owner", row.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("cloud download did not start")
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("cloud request was not canceled")
	}
	result := awaitPlayback(t, svc, row.ID)
	if result.PlaybackStatus != model.PlaybackStatusFailed {
		t.Fatalf("shutdown did not terminate processing: %#v", result)
	}
	entries, err := os.ReadDir(filepath.Join(svc.dataDir, playbackDirName))
	if err != nil || len(entries) != 0 {
		t.Fatalf("shutdown leaked files: %v, %v", entries, err)
	}
}

func TestPlaybackMaterializationRejectsPrivateUpstreamAndTraversal(t *testing.T) {
	svc, _ := newPlaybackTestService(t)
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "")
	setting := ossSettingValue{Provider: s3Provider, Endpoint: "http://127.0.0.1:1", Bucket: "bucket", Region: "us-east-1", AccessKeyID: "test-key", AccessKeySecret: "test-secret"}
	if stream, err := getOSSObjectRangeContext(context.Background(), setting, "clip", ""); err == nil {
		stream.body.Close()
		t.Fatal("private upstream bypassed outbound policy")
	}
	if err := os.MkdirAll(filepath.Join(svc.dataDir, "resources"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc.dataDir, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	row := &model.Resource{UserID: "owner", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "../secret"}
	if stream, err := svc.openResourceRangeContext(context.Background(), "owner", row, ""); err == nil {
		data, _ := io.ReadAll(stream.Body)
		stream.Body.Close()
		t.Fatalf("escaped local resource root: %q", data)
	}
}
