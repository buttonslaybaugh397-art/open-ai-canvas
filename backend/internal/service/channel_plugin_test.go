package service

import (
	"context"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestChannelPluginForMatchesDedicatedChannels(t *testing.T) {
	tests := []struct {
		name           string
		baseURL        string
		connectionType string
		wantID         string
	}{
		{name: "globalaiopc by URL", baseURL: "https://zcbservice.aizfw.cn/kyyReactApiServer/", wantID: "globalaiopc"},
		{name: "huiquyun by connection type", baseURL: "https://custom.example.com", connectionType: "huiquyun", wantID: "huiquyun"},
		{name: "aistarslab by URL", baseURL: "https://api.video.aistarslab.com/openapi/", wantID: "aistarslab"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plugin := ChannelPluginFor(test.baseURL, test.connectionType)
			if plugin == nil || plugin.ID != test.wantID {
				t.Fatalf("plugin = %#v, want %q", plugin, test.wantID)
			}
		})
	}
}

func TestChannelCatalogRespectsPluginState(t *testing.T) {
	center, err := newPluginRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := center.setEnabled("globalaiopc-channel", false); err != nil {
		t.Fatal(err)
	}
	svc := &Service{pluginRuntime: center}
	_, err = svc.FetchChannelModelCatalog(context.Background(), &model.User{ID: "user-1"}, ChannelModelsRequest{BaseURL: "https://zcbservice.aizfw.cn/kyyReactApiServer", APIKey: "test"})
	if err == nil || !strings.Contains(err.Error(), "插件未启用") {
		t.Fatalf("disabled plugin catalog error = %v", err)
	}
}

func TestNormalizeCustomRelayFormat(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "openai", want: "openai"},
		{input: "globalaiopc", want: "openai"},
		{input: "aistarslab", want: "openai"},
		{input: "gemini", want: "gemini"},
	} {
		got, ok := NormalizeCustomRelayFormat(test.input)
		if !ok || got != test.want {
			t.Fatalf("NormalizeCustomRelayFormat(%q) = %q, %v; want %q, true", test.input, got, ok, test.want)
		}
	}
	if _, ok := NormalizeCustomRelayFormat("unsupported"); ok {
		t.Fatal("unsupported relay format was accepted")
	}
}

func TestGlobalAiOpcCatalogIsStaticAndTyped(t *testing.T) {
	catalog := globalAiOpcCatalog()
	if len(catalog) != 17 {
		t.Fatalf("catalog length = %d, want 17", len(catalog))
	}
	if catalog[0].SupportedEndpointTypes[0] != "globalaiopc-image" || catalog[len(catalog)-1].SupportedEndpointTypes[0] != "globalaiopc-video" {
		t.Fatalf("catalog endpoint types = %#v", catalog)
	}
}
