package repository

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpsertCanvasProjectRejectsOlderSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:canvas-project-revision?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.CanvasProject{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	newerAt := time.Date(2026, 9, 1, 12, 0, 2, 0, time.UTC)
	newer := model.CanvasProject{ID: "canvas-1", UserID: "user-1", Title: "newer", PayloadJSON: `{"id":"canvas-1","title":"newer"}`, CreatedAt: newerAt.Add(-time.Hour), UpdatedAt: newerAt}
	accepted, err := repo.UpsertCanvasProject(&newer)
	if err != nil || !accepted {
		t.Fatalf("initial upsert accepted=%v err=%v", accepted, err)
	}

	older := newer
	older.Title = "older"
	older.PayloadJSON = `{"id":"canvas-1","title":"older"}`
	older.UpdatedAt = newerAt.Add(-time.Second)
	accepted, err = repo.UpsertCanvasProject(&older)
	if err != nil || accepted {
		t.Fatalf("older upsert accepted=%v err=%v", accepted, err)
	}

	stored, err := repo.CanvasProjectForUser("user-1", "canvas-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title != newer.Title || stored.PayloadJSON != newer.PayloadJSON || !stored.UpdatedAt.Equal(newerAt) {
		t.Fatalf("stored stale canvas: %#v", stored)
	}

	sameTimeDifferentContent := newer
	sameTimeDifferentContent.Title = "same-time overwrite"
	accepted, err = repo.UpsertCanvasProject(&sameTimeDifferentContent)
	if err != nil || accepted {
		t.Fatalf("same-time overwrite accepted=%v err=%v", accepted, err)
	}
	accepted, err = repo.UpsertCanvasProject(&newer)
	if err != nil || !accepted {
		t.Fatalf("idempotent upsert accepted=%v err=%v", accepted, err)
	}
}
