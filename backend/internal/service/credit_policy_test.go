package service

import (
	"testing"
	"time"
)

func TestCreditPolicyDayUsesBeijingCalendar(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want string
	}{
		{name: "before Beijing midnight", now: time.Date(2026, time.August, 21, 15, 59, 59, 0, time.UTC), want: "2026-08-21"},
		{name: "at Beijing midnight", now: time.Date(2026, time.August, 21, 16, 0, 0, 0, time.UTC), want: "2026-08-22"},
		{name: "after midnight", now: time.Date(2026, time.August, 21, 23, 30, 0, 0, time.FixedZone("UTC+4", 4*60*60)), want: "2026-08-22"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := creditPolicyDay(tc.now); got != tc.want {
				t.Fatalf("creditPolicyDay(%s) = %q, want %q", tc.now.Format(time.RFC3339), got, tc.want)
			}
		})
	}
}

func TestNormalizeCreditConsumptionFilterClampsLiveDayEnd(t *testing.T) {
	location := creditConsumptionLocation
	now := time.Date(2026, time.September, 2, 15, 30, 0, 0, location)

	filter := normalizeCreditConsumptionFilterAt(AdminCreditConsumptionQuery{
		From: "2026-09-01",
		To:   "2026-09-02",
	}, now)
	if !filter.From.Equal(time.Date(2026, time.September, 1, 0, 0, 0, 0, location)) {
		t.Fatalf("unexpected from: %s", filter.From.Format(time.RFC3339))
	}
	if !filter.To.Equal(now) {
		t.Fatalf("today upper bound = %s, want %s", filter.To.Format(time.RFC3339), now.Format(time.RFC3339))
	}

	historical := normalizeCreditConsumptionFilterAt(AdminCreditConsumptionQuery{
		From: "2026-08-01",
		To:   "2026-08-02",
	}, now)
	wantHistoricalTo := time.Date(2026, time.August, 3, 0, 0, 0, 0, location)
	if !historical.To.Equal(wantHistoricalTo) {
		t.Fatalf("historical upper bound = %s, want %s", historical.To.Format(time.RFC3339), wantHistoricalTo.Format(time.RFC3339))
	}

	future := normalizeCreditConsumptionFilterAt(AdminCreditConsumptionQuery{
		From: "2026-09-03",
		To:   "2026-09-04",
	}, now)
	if !future.From.Equal(now) || !future.To.Equal(now) {
		t.Fatalf("future range = [%s, %s), want an empty current-time range", future.From.Format(time.RFC3339), future.To.Format(time.RFC3339))
	}
}
