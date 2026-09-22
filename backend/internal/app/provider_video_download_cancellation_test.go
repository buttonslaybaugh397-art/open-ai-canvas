package app

import (
	"context"
	"errors"
	"testing"
)

func TestVideoDownloadStopsAfterDownloadCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, _, err := runVideoDownload(ctx, "provider-task-1", fastVideoPollPolicy(), func(context.Context) ([]byte, string, error) {
		cancel()
		return []byte("video"), "video/mp4", nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}
