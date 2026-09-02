package service

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestBuildAnalyticsOverviewVideoStatistics(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)
	logs := []model.ApiCallLog{
		{Capability: "video", Status: model.ApiCallStatusSucceeded, VideoSeconds: 5, EstimatedCostMicros: 500_000, CostAvailable: true, Currency: "USD"},
		{Capability: "video", Status: model.ApiCallStatusSucceeded, VideoSeconds: 10, EstimatedCostMicros: 1_500_000, CostAvailable: true, Currency: "USD"},
		{Capability: "text", Status: model.ApiCallStatusSucceeded, VideoSeconds: 99, EstimatedCostMicros: 9_999_999, CostAvailable: true, Currency: "USD"},
		{Capability: "video", Status: model.ApiCallStatusFailed, VideoSeconds: 20, EstimatedCostMicros: 2_000_000, CostAvailable: true, Currency: "USD"},
	}

	result := buildAnalyticsOverview(repository.AnalyticsFilter{From: from, To: to}, nil, nil, nil, logs, nil, nil)

	if result.KPI.TotalVideoSeconds != 15 {
		t.Fatalf("TotalVideoSeconds = %d, want 15", result.KPI.TotalVideoSeconds)
	}
	if result.KPI.AvgCostPerVideoSecondMicros != 133_333 {
		t.Fatalf("AvgCostPerVideoSecondMicros = %d, want 133333", result.KPI.AvgCostPerVideoSecondMicros)
	}
	if !result.KPI.VideoCostAvailable {
		t.Fatal("VideoCostAvailable = false, want true")
	}
	if result.KPI.VideoCurrency != "USD" {
		t.Fatalf("VideoCurrency = %q, want USD", result.KPI.VideoCurrency)
	}
}

func TestBuildAnalyticsOverviewVideoAverageRequiresCompletePricing(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)
	logs := []model.ApiCallLog{
		{Capability: "video", Status: model.ApiCallStatusSucceeded, VideoSeconds: 5, EstimatedCostMicros: 500_000, CostAvailable: true, Currency: "USD"},
		{Capability: "video", Status: model.ApiCallStatusSucceeded, VideoSeconds: 10},
	}

	result := buildAnalyticsOverview(repository.AnalyticsFilter{From: from, To: to}, nil, nil, nil, logs, nil, nil)

	if result.KPI.TotalVideoSeconds != 15 {
		t.Fatalf("TotalVideoSeconds = %d, want 15", result.KPI.TotalVideoSeconds)
	}
	if result.KPI.VideoCostAvailable {
		t.Fatal("VideoCostAvailable = true, want false")
	}
	if result.KPI.AvgCostPerVideoSecondMicros != 0 {
		t.Fatalf("AvgCostPerVideoSecondMicros = %d, want 0", result.KPI.AvgCostPerVideoSecondMicros)
	}
}
