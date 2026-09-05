package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
