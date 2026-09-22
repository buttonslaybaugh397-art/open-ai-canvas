package protocol

import (
	"context"
	"testing"
)

func TestYU25SeedanceManifestUsesOutputFieldsForValidation(t *testing.T) {
	adapter := yu25SeedanceManifestAdapter(t)
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model:  "seedance-2.5-pro",
		Prompt: "clip",
		Output: OutputOptions{Duration: 8, AspectRatio: "9:16", Resolution: "480p"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, spec)
	if body["seconds"] != float64(8) || body["aspect_ratio"] != "9:16" || body["resolution"] != "480p" {
		t.Fatalf("output fields were not mapped: %#v", body)
	}
}

func TestYU25SeedanceManifestTreatsStructuredBusinessErrorsAsFailed(t *testing.T) {
	adapter := yu25SeedanceManifestAdapter(t)
	for _, body := range []string{
		`{"error":{"code":"model_not_found","message":"model unavailable"}}`,
		`{"data":{"error":{"type":"quota_exceeded","detail":"quota"}}}`,
		`{"data":{"data":{"error":{"message":"upstream failed"}}}}`,
	} {
		result, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-error"}, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != StatusFailed {
			t.Fatalf("body %s parsed as %q, want failed", body, result.Status)
		}
	}
}

func TestYU25SeedanceManifestLoadsFromInstallationPackage(t *testing.T) {
	adapter := officialPackageAdapter(t, "yu25-seedance-video.yingce-plugin", "yu25-seedance-video")
	if adapter.Metadata().ID != "yu25-seedance-video" {
		t.Fatalf("installed provider id = %q", adapter.Metadata().ID)
	}
}
