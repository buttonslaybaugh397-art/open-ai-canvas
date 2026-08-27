package service

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
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

func TestHuiQuYunMjSd933MultipartBodyKeepsReferenceFiles(t *testing.T) {
	input := canvasGenerationInput{
		Prompt: "two frames",
		Config: providerConfig{
			InterfaceType: string(model.ChannelInterfaceHuiQuYunVideo),
			Model:         "mj-sd2.0-933-720p",
			Size:          "16:9",
			VQuality:      "720",
			VideoSeconds:  "8",
		},
		ReferenceImages: []providerMedia{
			{ID: "reference-1", DataURL: "data:image/png;base64,iVBORw0KGgo="},
			{ID: "reference-2", DataURL: "data:image/png;base64,iVBORw0KGgo="},
		},
	}
	body, contentType, err := huiQuYunMX933MultipartBody(input)
	if err != nil {
		t.Fatalf("huiQuYunMX933MultipartBody() error = %v", err)
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("ParseMediaType() error = %v", err)
	}
	reader := multipart.NewReader(body, params["boundary"])
	fields := map[string][]string{}
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatalf("NextPart() error = %v", nextErr)
		}
		value, readErr := io.ReadAll(part)
		if readErr != nil {
			t.Fatalf("ReadAll() error = %v", readErr)
		}
		fields[part.FormName()] = append(fields[part.FormName()], string(value))
	}
	if len(fields["images"]) != 2 {
		t.Fatalf("multipart images = %d", len(fields["images"]))
	}
	if values := fields["resolution"]; len(values) != 1 || values[0] != "720p" {
		t.Fatalf("multipart resolution = %#v", values)
	}
}

func TestFailedCreateDoesNotPersistEndpointAsProviderRequestID(t *testing.T) {
	log := &model.ApiCallLog{RequestKind: "create", Path: "/v1/videos/generations", Status: model.ApiCallStatusFailed}
	(&Service{}).EnrichAPICallLog(log, []byte(`{"code":"fail_to_fetch_task","message":"invalid_request"}`))
	if log.ProviderRequestID != "" {
		t.Fatalf("failed create provider request id = %q", log.ProviderRequestID)
	}
}
