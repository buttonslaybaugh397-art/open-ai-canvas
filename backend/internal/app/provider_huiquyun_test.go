package app

import (
	"context"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/protocol"
)

func TestHuiQuYun933ReferencesUseProtocolMultipartFiles(t *testing.T) {
	adapter, ok := protocol.Builtins().Get(string(model.ChannelInterfaceHuiQuYunVideo))
	if !ok {
		t.Fatal("HuiQuYun adapter is missing")
	}
	for _, modelName := range []string{"sd2-mx933-720-5s", "sd2-mx933-720-fast-10s", "mj-sd2.0-933-720p", "mj-sd2.0-933-720p-10s"} {
		input := canvasGenerationInput{
			Mode: "video", Config: providerConfig{InterfaceType: string(model.ChannelInterfaceHuiQuYunVideo), Model: modelName, VideoSeconds: "8", VQuality: "720"},
			ReferenceImages: []providerMedia{{ID: "reference-1", Name: "reference.png", DataURL: "data:image/png;base64,aW1hZ2U="}},
		}
		policy := providerMediaHydrationPolicyFor(context.Background(), input)
		if policy.requireURL || policy.preferURL {
			t.Fatalf("model %q discarded local multipart media", modelName)
		}
		request, err := prepareCustomProtocolRequest(input)
		if err != nil {
			t.Fatalf("prepare request for %q: %v", modelName, err)
		}
		spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{Request: request})
		if err != nil {
			t.Fatalf("build create for %q: %v", modelName, err)
		}
		if spec.ContentType != "multipart/form-data" || len(spec.Files) != 1 || spec.Files[0].Name != "images" {
			t.Fatalf("model %q spec = %#v", modelName, spec)
		}
		if spec.Body.(map[string]any)["resolution"] != "720p" {
			t.Fatalf("model %q resolution = %#v", modelName, spec.Body)
		}
	}
}

func TestHuiQuYunOrdinaryReferenceUsesPublicURL(t *testing.T) {
	input := canvasGenerationInput{
		Mode: "video", Config: providerConfig{InterfaceType: string(model.ChannelInterfaceHuiQuYunVideo), Model: "seedance-video"},
		ReferenceImages: []providerMedia{{ID: "reference-1"}},
	}
	policy := providerMediaHydrationPolicyFor(context.Background(), input)
	if !policy.requireURL || !policy.preferURL {
		t.Fatal("ordinary HuiQuYun JSON request did not require public media URL")
	}
}

func TestHuiQuYunMultipartKeepsFrameRoles(t *testing.T) {
	adapter, _ := protocol.Builtins().Get(string(model.ChannelInterfaceHuiQuYunVideo))
	input := canvasGenerationInput{
		Mode: "video", Config: providerConfig{InterfaceType: string(model.ChannelInterfaceHuiQuYunVideo), Model: "mj-sd2.0-933-720p", Size: "16:9", VQuality: "720", VideoSeconds: "8"},
		ReferenceImages: []providerMedia{
			{ID: "first", Name: "first.png", DataURL: "data:image/png;base64,aW1hZ2U="},
			{ID: "last", Name: "last.png", DataURL: "data:image/png;base64,aW1hZ2U="},
		},
		Metadata: map[string]interface{}{"videoStartFrameNodeId": "first", "videoEndFrameNodeId": "last"},
	}
	request, err := prepareCustomProtocolRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Files) != 2 || spec.Files[0].Name != "first_frame" || spec.Files[1].Name != "last_frame" {
		t.Fatalf("multipart files = %#v", spec.Files)
	}
}

func TestFailedCreateDoesNotPersistEndpointAsProviderRequestID(t *testing.T) {
	log := &model.ApiCallLog{RequestKind: "create", Path: "/v1/videos/generations", Status: model.ApiCallStatusFailed}
	(&Service{}).EnrichAPICallLog(log, []byte(`{"code":"fail_to_fetch_task","message":"invalid_request"}`))
	if log.ProviderRequestID != "" {
		t.Fatalf("failed create provider request id = %q", log.ProviderRequestID)
	}
}
