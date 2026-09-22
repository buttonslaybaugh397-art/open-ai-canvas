package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/protocol"
)

func TestYU25SeedanceInstalledPackageRunsCreatePollAndContentDownload(t *testing.T) {
	allowLoopbackProviderTest(t)
	packagePath := filepath.Join("..", "..", "..", "plugin-packages", "yu25-seedance-video.yingce-plugin")
	packageData, err := os.ReadFile(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := protocol.ParsePluginPackage(packageData)
	if err != nil {
		t.Fatal(err)
	}
	adapters, err := protocol.LoadInstalledProviders(pkg.ManifestRaw, nil)
	if err != nil || len(adapters) != 1 {
		t.Fatalf("load YU25 package provider: count=%d, error=%v", len(adapters), err)
	}
	adapter := adapters[0]

	createCalls, pollCalls, contentCalls := 0, 0, 0
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-yu25-key" {
			t.Errorf("%s %s authorization = %q", r.Method, r.URL.Path, got)
		}
		switch r.URL.Path {
		case "/v1/videos":
			createCalls++
			if r.Method != http.MethodPost {
				t.Errorf("create method = %s, want POST", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"task-1","status":"queued"}`))
		case "/v1/videos/task-1":
			pollCalls++
			if r.Method != http.MethodGet {
				t.Errorf("poll method = %s, want GET", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			if pollCalls == 1 {
				_, _ = w.Write([]byte(`{"id":"task-1","status":"processing"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"task-1","status":"completed"}`))
		case "/v1/videos/task-1/content":
			contentCalls++
			if r.Method != http.MethodGet || r.Header.Get("Accept") != "video/mp4" {
				t.Errorf("content request = %s Accept=%q, want GET video/mp4", r.Method, r.Header.Get("Accept"))
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("test-yu25-video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runProtocolAdapterTaskWithPolicy(ctx, canvasGenerationInput{
		Mode:            "video",
		Prompt:          "保持人物身份一致，镜头缓慢推进",
		Config:          providerConfig{BaseURL: server.URL, APIKey: "test-yu25-key", Model: "seedance-2.5-pro", InterfaceType: "yu25-seedance-video", VideoSeconds: "8", Size: "16:9", VQuality: "720p"},
		ReferenceImages: []providerMedia{{URL: "https://assets.example.com/person.jpg"}},
	}, adapter, fastVideoPollPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if createCalls != 1 || pollCalls != 2 || contentCalls != 1 {
		t.Fatalf("request counts = create:%d poll:%d content:%d, want 1, 2, 1", createCalls, pollCalls, contentCalls)
	}
	if receivedBody["model"] != "seedance-2.5-pro" || receivedBody["prompt"] != "保持人物身份一致，镜头缓慢推进" || receivedBody["seconds"] != float64(8) || receivedBody["resolution"] != "720p" || receivedBody["aspect_ratio"] != "16:9" {
		t.Fatalf("canonical create body = %#v", receivedBody)
	}
	images, _ := receivedBody["images"].([]any)
	if len(images) != 1 || images[0] != "https://assets.example.com/person.jpg" {
		t.Fatalf("reference images = %#v", images)
	}
	video, ok := result["video"].(map[string]interface{})
	if !ok || video["mimeType"] != "video/mp4" || video["dataUrl"] != "data:video/mp4;base64,"+base64.StdEncoding.EncodeToString([]byte("test-yu25-video")) {
		t.Fatalf("downloaded video result = %#v", result)
	}
	if strings.Contains(string(yu25TestJSON(t, receivedBody)), "apiKey") {
		t.Fatal("API key leaked into create body")
	}
}

func yu25TestJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
