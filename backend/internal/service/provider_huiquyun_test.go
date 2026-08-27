package service

import (
	"context"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestHuiQuYun933ReferencesUseMultipartFiles(t *testing.T) {
	for _, modelName := range []string{"sd2-mx933-720-5s", "sd2-mx933-720-fast-10s", "mj-sd2.0-933-720p", "mj-sd2.0-933-720p-10s"} {
		input := canvasGenerationInput{
			Config:          providerConfig{InterfaceType: string(model.ChannelInterfaceHuiQuYunVideo), Model: modelName},
			ReferenceImages: []providerMedia{{ID: "reference-1"}},
		}
		if !huiQuYunUsesMultipartVideoRequest(input) {
			t.Fatalf("model %q did not use multipart", modelName)
		}
		if generationRequiresPublicReferenceURL(context.Background(), input) {
			t.Fatalf("model %q discarded local multipart media", modelName)
		}
	}
}

func TestHuiQuYunOrdinaryReferenceUsesPublicURL(t *testing.T) {
	input := canvasGenerationInput{
		Config:          providerConfig{InterfaceType: string(model.ChannelInterfaceHuiQuYunVideo), Model: "seedance-video"},
		ReferenceImages: []providerMedia{{ID: "reference-1"}},
	}
	if huiQuYunUsesMultipartVideoRequest(input) {
		t.Fatal("ordinary HuiQuYun model unexpectedly used multipart")
	}
	if !generationRequiresPublicReferenceURL(context.Background(), input) {
		t.Fatal("ordinary HuiQuYun JSON request did not require public media URL")
	}
}

func TestFailedCreateDoesNotPersistEndpointAsProviderRequestID(t *testing.T) {
	log := &model.ApiCallLog{RequestKind: "create", Path: "/v1/videos/generations", Status: model.ApiCallStatusFailed}
	(&Service{}).EnrichAPICallLog(log, []byte(`{"code":"fail_to_fetch_task","message":"invalid_request"}`))
	if log.ProviderRequestID != "" {
		t.Fatalf("failed create provider request id = %q", log.ProviderRequestID)
	}
}
