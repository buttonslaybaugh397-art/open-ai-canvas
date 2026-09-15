package repository

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestResourcePlaybackClaimAndCompletionCAS(t *testing.T) {
	repo, db, resource, _, _ := newResourceRepairTestRepository(t)
	if err := db.Model(&model.Resource{}).Where("id = ?", resource.ID).UpdateColumn("kind", "video").Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ClaimResourcePlayback("other", resource.ID, "wrong.mp4"); err != nil || ok {
		t.Fatalf("foreign claim: %v %v", ok, err)
	}
	if ok, err := repo.ClaimResourcePlayback("owner", resource.ID, "claim.mp4"); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if ok, err := repo.ClaimResourcePlayback("owner", resource.ID, "duplicate.mp4"); err != nil || ok {
		t.Fatalf("duplicate claim: %v %v", ok, err)
	}
	if ok, err := repo.MarkResourcePlaybackNone("owner", resource.ID); err != nil || ok {
		t.Fatalf("automatic none clobbered processing: %v %v", ok, err)
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", resource.ID).UpdateColumn("mime_type", "concurrent/type").Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.CompleteResourcePlayback("owner", resource.ID, "stale.mp4", model.PlaybackStatusFailed, "stale"); err != nil || ok {
		t.Fatalf("stale completion: %v %v", ok, err)
	}
	if err := repo.ExpireResourcePlaybacks(time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.CompleteResourcePlayback("owner", resource.ID, "claim.mp4", model.PlaybackStatusReady, ""); err != nil || !ok {
		t.Fatalf("completion: %v %v", ok, err)
	}
	saved, err := repo.Resource(resource.ID)
	if err != nil || saved.MimeType != "concurrent/type" || saved.PlaybackObjectKey != "claim.mp4" || saved.PlaybackStatus != model.PlaybackStatusReady {
		t.Fatalf("completion overwrote unrelated fields: %#v, %v", saved, err)
	}
	if ok, err := repo.CompleteResourcePlayback("owner", resource.ID, "claim.mp4", model.PlaybackStatusFailed, "late"); err != nil || ok {
		t.Fatalf("late failure overwrote ready: %v %v", ok, err)
	}
}

func TestResourcePlaybackExpirationOnlyFailsStaleClaims(t *testing.T) {
	repo, db, resource, _, _ := newResourceRepairTestRepository(t)
	before := time.Now().Add(-time.Hour)
	if err := db.Model(&model.Resource{}).Where("id = ?", resource.ID).Updates(map[string]any{
		"kind": "video", "playback_status": model.PlaybackStatusProcessing, "playback_object_key": "stale.mp4", "updated_at": before.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.ExpireResourcePlaybacks(before); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.Resource(resource.ID)
	if err != nil || saved.PlaybackStatus != model.PlaybackStatusFailed || saved.PlaybackObjectKey != "" {
		t.Fatalf("stale claim not failed: %#v %v", saved, err)
	}
	if ok, err := repo.ClaimResourcePlayback("owner", resource.ID, "retry.mp4"); err != nil || ok {
		t.Fatalf("failed state was retried: %v %v", ok, err)
	}
	if ok, err := repo.CompleteResourcePlayback("owner", resource.ID, "stale.mp4", model.PlaybackStatusReady, ""); err != nil || ok {
		t.Fatalf("expired worker committed: %v %v", ok, err)
	}
}
