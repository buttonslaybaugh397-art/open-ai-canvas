package service

import "testing"

func TestGlobalAiOpcImageTaskBodyUsesChannelContract(t *testing.T) {
	body, err := globalAiOpcTaskBody(canvasGenerationInput{
		Mode:            "image",
		Prompt:          "draw",
		Config:          providerConfig{Model: "seedream_5.0Pro", Size: "16:9", Quality: "2k"},
		ReferenceImages: []providerMedia{{URL: "https://example.com/reference.png"}},
	}, "image")
	if err != nil {
		t.Fatal(err)
	}
	if body["aspect_ratio"] != "16:9" || body["resolution"] != "2K" {
		t.Fatalf("image body = %#v", body)
	}
	references, ok := body["reference_images"].([]string)
	if !ok || len(references) != 1 || references[0] != "https://example.com/reference.png" {
		t.Fatalf("reference_images = %#v", body["reference_images"])
	}
}

func TestGlobalAiOpcVideoTaskBodyKeepsSelectedResolutionAndReferences(t *testing.T) {
	profile := DefaultModelCapabilityConfigForModel("globalaiopc-video", "seedance-2.5-c1").Video
	body, err := globalAiOpcTaskBody(canvasGenerationInput{
		Mode:   "video",
		Prompt: "animate",
		Config: providerConfig{Model: "seedance-2.5-c1", Size: "16:9", VideoSeconds: "8", VQuality: "480p", VideoGenerateAudio: "true", VideoWatermark: "false"},
		ReferenceImages: []providerMedia{
			{ID: "start", URL: "https://example.com/start.png"},
			{ID: "end", URL: "https://example.com/end.png"},
		},
		ReferenceVideos: []providerMedia{{URL: "https://example.com/reference.mp4"}},
		ReferenceAudios: []providerMedia{{URL: "https://example.com/reference.mp3"}},
		Metadata:        map[string]interface{}{"videoStartFrameNodeId": "start", "videoEndFrameNodeId": "end"},
		VideoCapability: profile,
	}, "video")
	if err != nil {
		t.Fatal(err)
	}
	if body["resolution"] != "480p" || body["duration"] != 8 || body["first_image"] != "https://example.com/start.png" || body["last_image"] != "https://example.com/end.png" {
		t.Fatalf("video body = %#v", body)
	}
	if videos, ok := body["reference_videos"].([]string); !ok || len(videos) != 1 {
		t.Fatalf("reference_videos = %#v", body["reference_videos"])
	}
	if audios, ok := body["reference_audios"].([]string); !ok || len(audios) != 1 {
		t.Fatalf("reference_audios = %#v", body["reference_audios"])
	}
}

func TestGlobalAiOpcSeedance15UsesSizeAndFrameFields(t *testing.T) {
	body, err := globalAiOpcTaskBody(canvasGenerationInput{
		Mode:   "video",
		Prompt: "animate",
		Config: providerConfig{Model: "seedance_1_5_pro_720p", Size: "9:16", VideoSeconds: "5", VQuality: "480p"},
		ReferenceImages: []providerMedia{
			{URL: "https://example.com/start.png"},
			{URL: "https://example.com/end.png"},
		},
	}, "video")
	if err != nil {
		t.Fatal(err)
	}
	if body["size"] != "9:16" || body["first_image"] != "https://example.com/start.png" || body["last_image"] != "https://example.com/end.png" {
		t.Fatalf("seedance 1.5 body = %#v", body)
	}
	if _, exists := body["resolution"]; exists {
		t.Fatalf("seedance 1.5 body unexpectedly contains resolution: %#v", body)
	}
	if _, exists := body["reference_images"]; exists {
		t.Fatalf("seedance 1.5 body unexpectedly contains reference_images: %#v", body)
	}
}
