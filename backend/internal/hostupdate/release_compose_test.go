package hostupdate

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type composeRoundTripper func(*http.Request) (*http.Response, error)

func (f composeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReleaseComposeDoesNotRenameLocalCompose(t *testing.T) {
	directory := t.TempDir()
	localPath := filepath.Join(directory, "docker-compose.yml")
	if err := os.WriteFile(localPath, []byte("original deployment"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(Config{
		Repository: "test/canvas", InstallDir: directory, StateDir: filepath.Join(directory, "state"),
		ComposeFile: "docker-compose.yml", ReleaseComposeFile: "docker-compose.1panel.yml",
	})
	if err != nil {
		t.Fatal(err)
	}
	manager.httpClient = &http.Client{Transport: composeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.Path; got != "/test/canvas/v1.2.8/docker-compose.1panel.yml" {
			t.Fatalf("unexpected release path: %s", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("name: open-ai-canvas\n")), Header: make(http.Header)}, nil
	})}
	next, err := manager.prepareTargetCompose("v1.2.8")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(next)
	if err != nil || string(data) != "name: open-ai-canvas\n" {
		t.Fatalf("release compose = %q, %v", data, err)
	}
	data, err = os.ReadFile(localPath)
	if err != nil || string(data) != "original deployment" || manager.composePath() != localPath {
		t.Fatal("preparing the target must preserve the local compose path and content")
	}
}

func TestReleaseComposeDefaultsAndValidation(t *testing.T) {
	directory := t.TempDir()
	config := Config{InstallDir: directory, StateDir: filepath.Join(directory, "state"), ComposeFile: "docker-compose.1panel.yml"}
	manager, err := NewManager(config)
	if err != nil || manager.config.ReleaseComposeFile != config.ComposeFile {
		t.Fatalf("release compose did not default to local filename: %v", err)
	}
	for _, name := range []string{"../compose.yml", `dir\compose.yml`, "compose.yml?raw=1", "compose.yml#fragment", "compose.txt"} {
		config.ReleaseComposeFile = name
		if _, err := NewManager(config); err == nil {
			t.Fatalf("invalid release compose accepted: %s", name)
		}
	}
}
