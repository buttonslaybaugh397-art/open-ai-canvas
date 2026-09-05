package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestChannelSlotFailureDetailsSeparatesRedisAOFFailureFromConcurrency(t *testing.T) {
	err := channelSlotError{
		scope: "098612e7e23ff3d452fdb854eba4cbb4",
		limit: 999,
		err:   runtimeCoordinationError{err: errors.New("MISCONF Errors writing to the AOF file: No space left on device")},
	}

	code, message := ChannelSlotFailureDetails(err)
	if code != "redis_persistence_unavailable" {
		t.Fatalf("error code = %q, want redis_persistence_unavailable", code)
	}
	if !strings.Contains(message, "Redis 持久化不可用") {
		t.Fatalf("message = %q, want Redis persistence diagnosis", message)
	}
	if strings.Contains(message, "并发上限 999") {
		t.Fatalf("message = %q, must not misdiagnose AOF failure as concurrency exhaustion", message)
	}
}

func TestChannelSlotFailureDetailsSeparatesRedisCoordinationFailure(t *testing.T) {
	err := channelSlotError{scope: "channel-1", limit: 3, err: runtimeCoordinationError{err: errors.New("dial tcp: connection refused")}}
	code, message := ChannelSlotFailureDetails(err)
	if code != "runtime_coordination_unavailable" {
		t.Fatalf("error code = %q, want runtime_coordination_unavailable", code)
	}
	if !strings.Contains(message, "运行时协调器不可用") {
		t.Fatalf("message = %q, want coordination diagnosis", message)
	}
}

func TestRuntimeCoordinatorWaitsUntilChannelSlotIsReleased(t *testing.T) {
	coordinator := &runtimeCoordinator{instanceID: "test", localRate: map[string]localRateEntry{}, localSlots: map[string]map[string]time.Time{}}
	releaseFirst, acquired, err := coordinator.acquire(context.Background(), "channel:one", 1, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("first acquire = (%v, %v), want acquired", acquired, err)
	}

	result := make(chan error, 1)
	go func() {
		releaseSecond, waitErr := coordinator.acquireWithWait(context.Background(), "channel:one", 1, time.Minute)
		if waitErr == nil {
			releaseSecond()
		}
		result <- waitErr
	}()

	select {
	case err := <-result:
		t.Fatalf("second acquire returned before release: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	releaseFirst()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("second acquire after release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second acquire did not resume after release")
	}
}

func TestRuntimeCoordinatorStopsWaitingWhenContextIsCancelled(t *testing.T) {
	coordinator := &runtimeCoordinator{instanceID: "test", localRate: map[string]localRateEntry{}, localSlots: map[string]map[string]time.Time{}}
	release, acquired, err := coordinator.acquire(context.Background(), "channel:one", 1, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("first acquire = (%v, %v), want acquired", acquired, err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := coordinator.acquireWithWait(ctx, "channel:one", 1, time.Minute); err == nil {
		t.Fatal("acquireWithWait() error = nil after cancellation")
	}
}
