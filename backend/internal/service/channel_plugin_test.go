package service

import "testing"

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
