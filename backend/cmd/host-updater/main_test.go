package main

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveTokenReadsSharedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "token")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("shared-token-123456789012345678901234567890\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := resolveToken("", path, true, false)
	if err != nil || token != "shared-token-123456789012345678901234567890" {
		t.Fatalf("token = %q, err = %v", token, err)
	}
}

func TestResolveTokenGeneratesOnlyForComposeRuntime(t *testing.T) {
	token, err := resolveToken("", filepath.Join(t.TempDir(), "missing-token"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 64 {
		t.Fatalf("generated token length = %d", len(token))
	}
	if _, err := hex.DecodeString(token); err != nil {
		t.Fatalf("generated token is not hexadecimal: %v", err)
	}
	if token, err := resolveToken("", filepath.Join(t.TempDir(), "missing-token"), false, false); err != nil || token != "" {
		t.Fatalf("non-Compose token = %q, err = %v", token, err)
	}
	if token, err := resolveToken("", filepath.Join(t.TempDir(), "missing-token"), true, true); err != nil || token != "" {
		t.Fatalf("configuration token = %q, err = %v", token, err)
	}
}

func TestResolveTokenReportsUnreadableTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token-directory")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := resolveToken("", path, true, false)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected token read error, got %v", err)
	}
}

func TestPersistTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "token")
	if err := persistTokenFile(path, "token-value-123456789012345678901234567890"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "token-value-123456789012345678901234567890" {
		t.Fatalf("unexpected token file content: %q", string(data))
	}
}

func TestDetectComposeFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := detectComposeFile(dir); err == nil {
		t.Fatal("missing Compose file was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := detectComposeFile(dir)
	if err != nil || name != "docker-compose.yml" {
		t.Fatalf("detected %q: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.container.yml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := detectComposeFile(dir); err == nil {
		t.Fatal("ambiguous Compose files were accepted")
	}
}

func TestPersistTokenFileAfterRuntimeDirectoryLoss(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "run", "token")
	token := strings.Repeat("a", 32)
	for attempt := 0; attempt < 2; attempt++ {
		if err := persistTokenFile(path, token); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o444 {
			t.Fatalf("token mode = %o", info.Mode().Perm())
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Dir(path)); err != nil {
			t.Fatal(err)
		}
	}
}
