package hostupdate

import (
	"archive/tar"
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type recordingRunner struct {
	calls      [][]string
	inputBytes int64
}

func (r *recordingRunner) Run(_ context.Context, _ string, args, _ []string, stdout, _ io.Writer) error {
	r.calls = append(r.calls, append([]string(nil), args...))
	if stdout == nil {
		return nil
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, " pg_dump ") {
		_, _ = io.WriteString(stdout, "PGDMPfixture")
		return nil
	}
	if strings.Contains(joined, "tar -C ") {
		archive := tar.NewWriter(stdout)
		_ = archive.WriteHeader(&tar.Header{Name: "fixture", Mode: 0o600, Size: 1})
		_, _ = archive.Write([]byte("x"))
		_ = archive.Close()
	}
	return nil
}

func (r *recordingRunner) RunWithInput(_ context.Context, _ string, args, _ []string, input io.Reader, _, _ io.Writer) error {
	r.calls = append(r.calls, append([]string(nil), args...))
	written, err := io.Copy(io.Discard, input)
	r.inputBytes += written
	return err
}

func TestSetEnvValuePreservesOtherSettings(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, ".env")
	if err := os.WriteFile(path, []byte("# keep\nCANVAS_IMAGE_TAG=1.0.0\nPOSTGRES_DB=canvas\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := setEnvValue(path, "CANVAS_IMAGE_TAG", "1.2.2-preview.1"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value := string(data)
	if !strings.Contains(value, "# keep\n") || !strings.Contains(value, "POSTGRES_DB=canvas\n") || !strings.Contains(value, "CANVAS_IMAGE_TAG=1.2.2-preview.1\n") {
		t.Fatalf("unexpected env contents: %q", value)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && stat.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%o, want 640", stat.Mode().Perm())
	}
}

func TestVerifyZipBackupRejectsCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, content := range map[string]string{
		"metadata.json":    "{}",
		"database.dump":    "database",
		"backend-data.tar": "data",
	} {
		entry, createErr := archive.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := io.WriteString(entry, content); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	checksum := "sha256:" + hex.EncodeToString(hash[:])
	if err := verifyZipBackup(path, checksum, defaultArchiveMaxBytes); err != nil {
		t.Fatalf("valid backup rejected: %v", err)
	}
	if err := os.WriteFile(path, append(data, byte(1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyZipBackup(path, checksum, defaultArchiveMaxBytes); err == nil {
		t.Fatal("corrupted backup was accepted")
	}
}

func TestCurrentVersionRejectsLatest(t *testing.T) {
	installDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(installDir, ".env"), []byte("CANVAS_IMAGE_TAG=latest\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{config: Config{InstallDir: installDir, EnvFile: ".env"}}
	if _, err := manager.currentVersion(); err == nil {
		t.Fatal("latest tag was accepted")
	}
}

func TestCreateBackupReadsBackendDataAsRoot(t *testing.T) {
	installDir := t.TempDir()
	backupDir := filepath.Join(installDir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, ".env"), []byte("POSTGRES_USER=canvas\nPOSTGRES_DB=canvas\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "docker-compose.deploy.yml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	manager := &Manager{
		config: Config{InstallDir: installDir, ComposeFile: "docker-compose.deploy.yml", EnvFile: ".env", BackupDir: backupDir},
		runner: runner,
	}
	backup, err := manager.createBackup("v1.2.2-preview.2")
	if err != nil {
		t.Fatal(err)
	}
	wanted := []string{
		"run --rm --no-deps --entrypoint sh --user root backend -c tar -C /data -cf - .",
		"exec -T redis redis-cli SAVE",
		"run --rm --no-deps --entrypoint sh --user root redis -c tar -C /data -cf - .",
		"run --rm --no-deps --entrypoint sh --user root postgres -c if grep -qs ' /run/canvas-secrets '",
	}
	found := make([]bool, len(wanted))
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		for index, expected := range wanted {
			if strings.Contains(joined, expected) {
				found[index] = true
			}
		}
	}
	for index, present := range found {
		if !present {
			t.Fatalf("backup did not run %q: %#v", wanted[index], runner.calls)
		}
	}
	if err := manager.restoreDeploymentSecrets(backup); err != nil {
		t.Fatal(err)
	}
	if runner.inputBytes == 0 {
		t.Fatal("deployment secrets archive was not streamed to the restore container")
	}
	restoreCommandFound := false
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "deployment-secrets volume is not mounted") {
			restoreCommandFound = true
			break
		}
	}
	if !restoreCommandFound {
		t.Fatalf("deployment secrets restore did not enforce the volume mount: %#v", runner.calls)
	}
}

func TestRollbackRejectsInvalidBackupBeforeStoppingServices(t *testing.T) {
	stateDir := t.TempDir()
	backupPath := filepath.Join(stateDir, "corrupt.zip")
	data := []byte("not-a-zip")
	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	runner := &recordingRunner{}
	manager := &Manager{
		config: Config{
			InstallDir:        stateDir,
			ComposeFile:       "docker-compose.deploy.yml",
			EnvFile:           ".env",
			StateDir:          stateDir,
			MigrationMaxBytes: defaultArchiveMaxBytes,
		},
		runner: runner,
		state:  persistedState{Operation: Operation{Phase: PhaseRollingBack}},
	}
	manager.runRollback("v1.2.5", Backup{Path: backupPath, Checksum: "sha256:" + hex.EncodeToString(hash[:])}, true)
	if manager.state.Operation.Phase != PhaseManualIntervention {
		t.Fatalf("phase=%s, want %s", manager.state.Operation.Phase, PhaseManualIntervention)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("invalid backup changed service state: %#v", runner.calls)
	}
}

func TestVerifyZipBackupRequiresServiceSnapshotForFormatTwo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup-v2.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, content := range map[string]string{
		"metadata.json":    "{\"format\":2}",
		"database.dump":    "database",
		"backend-data.tar": "data",
	} {
		entry, createErr := archive.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := io.WriteString(entry, content); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	checksum := "sha256:" + hex.EncodeToString(hash[:])
	if err := verifyZipBackup(path, checksum, defaultArchiveMaxBytes); err == nil {
		t.Fatal("format 2 backup without service snapshots was accepted")
	}
}

func TestCheckWritableDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := checkWritableDirectory(directory); err != nil {
		t.Fatalf("writable directory rejected: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("write probe was not cleaned up: %v", entries)
	}
	if err := checkWritableDirectory(filepath.Join(directory, "missing")); err == nil {
		t.Fatal("missing directory was accepted")
	}
}
