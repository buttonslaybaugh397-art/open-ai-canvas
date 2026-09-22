package app

import (
	"context"
	"testing"
	"time"
)

func TestProviderRequestTimeoutCapsLongTaskDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	if got := providerRequestTimeout(ctx); got != providerHTTPTimeout {
		t.Fatalf("timeout = %s, want provider HTTP cap %s", got, providerHTTPTimeout)
	}
}

func TestProviderRequestTimeoutUsesShorterTaskDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	got := providerRequestTimeout(ctx)
	if got <= 0 || got > 100*time.Millisecond {
		t.Fatalf("timeout = %s, want a positive value no greater than task deadline", got)
	}
}

func TestProviderChannelSlotWaitTimeoutMatchesPollInterval(t *testing.T) {
	if providerChannelSlotWaitTimeout != defaultVideoPollInterval {
		t.Fatalf("slot wait timeout = %s, want poll interval %s", providerChannelSlotWaitTimeout, defaultVideoPollInterval)
	}
}
