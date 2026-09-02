package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/protocol"
)

func TestPrepareWeijinGenerationMediaUploadsReferences(t *testing.T) {
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/source.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("image-bytes"))
		case "/source.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("video-bytes"))
		case "/api/upload/video":
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Fatalf("FormFile(file): %v", err)
			}
			defer file.Close()
			if header.Filename == "" {
				t.Fatal("uploaded filename is empty")
			}
			if data, _ := io.ReadAll(file); len(data) == 0 {
				t.Fatal("uploaded file is empty")
			}
			index := uploads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, "{\"url\":\"https://www.weijinapi.top/weijin-images/upload-%d.bin\"}", index)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := providerConfig{BaseURL: server.URL, APIKey: "test-key", InterfaceType: string(model.ChannelInterfaceWeijinVideo), AllowLocalChannel: true}
	ctx := withProviderOutboundPolicy(context.Background(), config)
	input := canvasGenerationInput{
		Config: config,
		ReferenceImages: []providerMedia{
			{ID: "image-1", Name: "first.png", URL: server.URL + "/source.png", MimeType: "image/png"},
			{ID: "image-2", Name: "duplicate.png", URL: server.URL + "/source.png", MimeType: "image/png"},
		},
		ReferenceVideos: []providerMedia{{ID: "video-1", Name: "clip.mp4", URL: server.URL + "/source.mp4", MimeType: "video/mp4"}},
	}
	prepared, err := prepareWeijinGenerationMedia(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if uploads.Load() != 2 {
		t.Fatalf("uploads = %d, want 2", uploads.Load())
	}
	if prepared.ReferenceImages[0].URL == "" || prepared.ReferenceImages[0].URL != prepared.ReferenceImages[1].URL {
		t.Fatalf("image URLs = %#v", prepared.ReferenceImages)
	}
	if !strings.HasPrefix(prepared.ReferenceVideos[0].URL, "https://www.weijinapi.top/weijin-images/") {
		t.Fatalf("video URL = %q", prepared.ReferenceVideos[0].URL)
	}
	if prepared.ReferenceImages[0].DataURL != "" || prepared.ReferenceVideos[0].DataURL != "" {
		t.Fatal("prepared references must not retain data URLs")
	}
}

func TestPrepareWeijinGenerationMediaReportsUploadFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/source.png" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("image-bytes"))
			return
		}
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	defer server.Close()
	config := providerConfig{BaseURL: server.URL, APIKey: "test-key", InterfaceType: string(model.ChannelInterfaceWeijinVideo), AllowLocalChannel: true}
	_, err := prepareWeijinGenerationMedia(withProviderOutboundPolicy(context.Background(), config), canvasGenerationInput{
		Config:          config,
		ReferenceImages: []providerMedia{{ID: "image-1", URL: server.URL + "/source.png"}},
	})
	if err == nil || !strings.Contains(err.Error(), "微进参考素材上传失败") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunProtocolAdapterTaskResumeSkipsWeijinUpload(t *testing.T) {
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/upload/video" {
			uploads.Add(1)
			t.Fatal("resume must not upload references")
		}
		if r.URL.Path == "/v1/videos/task-existing" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"id\":\"task-existing\",\"status\":\"failed\",\"error\":{\"message\":\"stopped\"}}"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	adapter, ok := protocol.Builtins().Get("weijin-video")
	if !ok {
		t.Fatal("weijin adapter is missing")
	}
	config := providerConfig{BaseURL: server.URL, APIKey: "test-key", InterfaceType: string(model.ChannelInterfaceWeijinVideo), AllowLocalChannel: true, Model: "dreamina-2.0-720p"}
	ctx := context.WithValue(context.Background(), providerAnalyticsKey{}, providerAnalyticsContext{ProviderRequestID: "task-existing"})
	ctx = withProviderOutboundPolicy(ctx, config)
	_, err := runProtocolAdapterTask(ctx, canvasGenerationInput{
		Mode:            "video",
		Prompt:          "test",
		Config:          config,
		ReferenceImages: []providerMedia{{ID: "image-1", URL: "https://invalid.example/source.png"}},
	}, adapter)
	if err == nil {
		t.Fatal("failed resumed task must return an error")
	}
	if uploads.Load() != 0 {
		t.Fatalf("uploads = %d, want 0", uploads.Load())
	}
}
