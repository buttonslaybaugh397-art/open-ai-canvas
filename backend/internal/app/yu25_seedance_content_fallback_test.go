package app

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"infinite-canvas/backend/internal/protocol"
)

func TestYU25SeedanceContentDownloadFallsBackToReturnedURL(t *testing.T) {
	allowLoopbackProviderTest(t)
	packageData, err := os.ReadFile(filepath.Join("..", "..", "..", "plugin-packages", "yu25-seedance-video.yingce-plugin"))
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

	contentCalls, urlCalls := 0, 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/task-1/content":
			contentCalls++
			http.Error(w, "content unavailable", http.StatusGone)
		case "/returned-url.mp4":
			urlCalls++
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("fallback-video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	input := canvasGenerationInput{
		Mode: "video",
		Config: providerConfig{
			BaseURL:       server.URL,
			APIKey:        "test-yu25-key",
			InterfaceType: "yu25-seedance-video",
		},
	}
	result, err := finishProtocolAdapterResult(
		context.Background(),
		input,
		adapters[0],
		protocol.GenerationRequest{},
		"task-1",
		&protocol.Result{Videos: []protocol.MediaReference{{URL: server.URL + "/returned-url.mp4"}}},
		fastVideoPollPolicy(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if contentCalls != 1 || urlCalls != 1 {
		t.Fatalf("content calls=%d url calls=%d, want one each", contentCalls, urlCalls)
	}
	video, ok := result["video"].(map[string]interface{})
	wantDataURL := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("fallback-video"))
	if !ok || video["dataUrl"] != wantDataURL {
		t.Fatalf("fallback result = %#v", result)
	}

	var downloadErr videoDownloadError
	if errors.As(err, &downloadErr) {
		t.Fatalf("unexpected download error: %v", err)
	}
}
