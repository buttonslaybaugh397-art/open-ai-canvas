package repository

import (
	"math"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreditConsumptionsUsesLocalTimeBoundariesAndSettledAmounts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:finance-consumption?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.BillingOrder{}); err != nil {
		t.Fatal(err)
	}

	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, time.August, 21, 15, 0, 0, 0, location)
	todayStart := time.Date(2026, time.August, 21, 0, 0, 0, 0, location)
	window := CreditConsumptionWindow{
		TodayFrom:     todayStart,
		YesterdayFrom: todayStart.AddDate(0, 0, -1),
		TodayTo:       now,
		WeekFrom:      time.Date(2026, time.August, 17, 0, 0, 0, 0, location),
		MonthFrom:     time.Date(2026, time.August, 1, 0, 0, 0, 0, location),
	}
	orders := []model.BillingOrder{
		{ID: "today", UserID: "user-1", IdempotencyKey: "today", AmountMicrocredits: 1_500_000, ActualAmountMicrocredits: 1_200_000, Status: model.BillingStatusSettled, SettledAt: timePointer(now.Add(-time.Hour))},
		{ID: "zero-usage", UserID: "user-1", IdempotencyKey: "zero-usage", AmountMicrocredits: 700_000, ActualAmountMicrocredits: 0, UsageAvailable: true, Status: model.BillingStatusSettled, SettledAt: timePointer(now.Add(-2 * time.Hour))},
		{ID: "beijing-day", UserID: "user-1", IdempotencyKey: "beijing-day", AmountMicrocredits: 250_000, Status: model.BillingStatusSettled, SettledAt: timePointer(time.Date(2026, time.August, 20, 23, 30, 0, 0, time.UTC))},
		{ID: "future", UserID: "user-1", IdempotencyKey: "future", AmountMicrocredits: 8_000_000, Status: model.BillingStatusSettled, SettledAt: timePointer(now.Add(time.Hour))},
		{ID: "yesterday", UserID: "user-1", IdempotencyKey: "yesterday", AmountMicrocredits: 500_000, Status: model.BillingStatusSettled, SettledAt: timePointer(time.Date(2026, time.August, 20, 12, 0, 0, 0, location))},
		{ID: "week", UserID: "user-1", IdempotencyKey: "week", AmountMicrocredits: 1_000_000, Status: model.BillingStatusSettled, SettledAt: timePointer(time.Date(2026, time.August, 17, 10, 0, 0, 0, location))},
		{ID: "month", UserID: "user-1", IdempotencyKey: "month", AmountMicrocredits: 2_000_000, Status: model.BillingStatusSettled, SettledAt: timePointer(time.Date(2026, time.August, 1, 10, 0, 0, 0, location))},
		{ID: "refunded", UserID: "user-1", IdempotencyKey: "refunded", AmountMicrocredits: 9_000_000, Status: model.BillingStatusRefunded, SettledAt: timePointer(now.Add(-time.Hour))},
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatal(err)
	}

	totals, err := (&Repository{db: db}).CreditConsumptions([]string{"user-1"}, window)
	if err != nil {
		t.Fatal(err)
	}
	got := totals["user-1"]
	if got.Today != 1_450_000 || got.Yesterday != 500_000 || got.Week != 2_950_000 || got.Month != 4_950_000 {
		t.Fatalf("unexpected consumption totals: %#v", got)
	}
}

func TestAdminCreditConsumptionAggregatesUsersModelsAndTrend(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:admin-credit-consumption?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.BillingOrder{}, &model.ApiCallLog{}, &model.CreditLedgerEntry{}); err != nil {
		t.Fatal(err)
	}
	users := []model.User{
		{ID: "user-1", Username: "first", DisplayName: "First User"},
		{ID: "user-2", Username: "second", DisplayName: "Second User"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	from := time.Date(2026, time.August, 1, 0, 0, 0, 0, location)
	to := time.Date(2026, time.September, 1, 0, 0, 0, 0, location)
	orders := []model.BillingOrder{
		{ID: "order-1", UserID: "user-1", IdempotencyKey: "order-1", Model: "image-pro", Capability: "image", AmountMicrocredits: 1_000_000, ActualAmountMicrocredits: 800_000, Status: model.BillingStatusSettled, SettledAt: timePointer(time.Date(2026, time.August, 20, 10, 0, 0, 0, location))},
		{ID: "order-2", UserID: "user-1", IdempotencyKey: "order-2", Model: "video-pro", Capability: "video", AmountMicrocredits: 2_000_000, Status: model.BillingStatusSettled, SettledAt: timePointer(time.Date(2026, time.August, 21, 11, 0, 0, 0, location))},
		{ID: "order-3", UserID: "user-2", IdempotencyKey: "order-3", Model: "image-pro", Capability: "image", AmountMicrocredits: 500_000, Status: model.BillingStatusSettled, SettledAt: timePointer(time.Date(2026, time.August, 21, 12, 0, 0, 0, location))},
		{ID: "old-order", UserID: "user-2", IdempotencyKey: "old-order", Model: "legacy", Capability: "image", AmountMicrocredits: 300_000, Status: model.BillingStatusSettled, SettledAt: timePointer(time.Date(2026, time.July, 1, 12, 0, 0, 0, location))},
		{ID: "refunded", UserID: "user-2", IdempotencyKey: "refunded", Model: "video-pro", Capability: "video", AmountMicrocredits: 9_000_000, Status: model.BillingStatusRefunded, RefundedAt: timePointer(time.Date(2026, time.August, 21, 13, 0, 0, 0, location))},
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatal(err)
	}

	repo := &Repository{db: db}
	filter := CreditConsumptionFilter{From: from, To: to}
	summary, err := repo.AdminCreditConsumptionSummary(filter)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AllTimeMicrocredits != 3_600_000 || summary.PeriodMicrocredits != 3_300_000 || summary.SettledOrders != 3 || summary.ConsumingUsers != 2 || summary.UsedModels != 2 ||
		summary.PreviousPeriodMicrocredits != 300_000 || summary.PreviousSettledOrders != 1 || summary.PreviousConsumingUsers != 1 || summary.PreviousUsedModels != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	trend, err := repo.AdminCreditConsumptionTrend(filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(trend) != 31 || trend[0].Day != "2026-08-01" || trend[0].TotalMicrocredits != 0 || trend[19].Day != "2026-08-20" || trend[19].TotalMicrocredits != 800_000 || trend[20].Day != "2026-08-21" || trend[20].TotalMicrocredits != 2_500_000 || trend[30].Day != "2026-08-31" || trend[30].TotalMicrocredits != 0 {
		t.Fatalf("unexpected trend: %#v", trend)
	}
	capabilityRows, err := repo.AdminCreditConsumptionCapabilities(filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(capabilityRows) != 2 || capabilityRows[0].Capability != "video" || capabilityRows[0].TotalMicrocredits != 2_000_000 ||
		capabilityRows[1].Capability != "image" || capabilityRows[1].TotalMicrocredits != 1_300_000 || capabilityRows[1].UniqueUsers != 2 {
		t.Fatalf("unexpected capability rows: %#v", capabilityRows)
	}
	userRows, userTotal, err := repo.AdminCreditConsumptionUsers(filter, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if userTotal != 2 || len(userRows) != 2 || userRows[0].UserID != "user-1" || userRows[0].TotalMicrocredits != 2_800_000 || userRows[0].ModelCount != 2 {
		t.Fatalf("unexpected user rows: total=%d rows=%#v", userTotal, userRows)
	}
	modelRows, modelTotal, err := repo.AdminCreditConsumptionModels(filter, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if modelTotal != 2 || len(modelRows) != 2 || modelRows[0].Model != "video-pro" || modelRows[0].TotalMicrocredits != 2_000_000 || modelRows[1].Model != "image-pro" || modelRows[1].UniqueUsers != 2 {
		t.Fatalf("unexpected model rows: total=%d rows=%#v", modelTotal, modelRows)
	}

	filter.UserID = "user-2"
	filteredSummary, err := repo.AdminCreditConsumptionSummary(filter)
	if err != nil {
		t.Fatal(err)
	}
	if filteredSummary.AllTimeMicrocredits != 800_000 || filteredSummary.PeriodMicrocredits != 500_000 || filteredSummary.ConsumingUsers != 1 {
		t.Fatalf("unexpected filtered summary: %#v", filteredSummary)
	}
}

func TestAdminCreditConsumptionTrendUsesBeijingDaysAndExcludesEndBoundary(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:admin-credit-consumption-boundaries?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.BillingOrder{}); err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	from := time.Date(2026, time.August, 1, 0, 0, 0, 0, location)
	to := time.Date(2026, time.August, 4, 0, 0, 0, 0, location)
	orders := []model.BillingOrder{
		{ID: "boundary-before-midnight", UserID: "user-1", IdempotencyKey: "boundary-before-midnight", AmountMicrocredits: 100, Status: model.BillingStatusSettled, SettledAt: timePointer(time.Date(2026, time.August, 1, 23, 59, 59, 0, location))},
		{ID: "boundary-after-midnight", UserID: "user-1", IdempotencyKey: "boundary-after-midnight", AmountMicrocredits: 200, Status: model.BillingStatusSettled, SettledAt: timePointer(time.Date(2026, time.August, 2, 0, 0, 1, 0, location))},
		{ID: "boundary-end", UserID: "user-1", IdempotencyKey: "boundary-end", AmountMicrocredits: 900, Status: model.BillingStatusSettled, SettledAt: timePointer(to)},
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatal(err)
	}

	trend, err := (&Repository{db: db}).AdminCreditConsumptionTrend(CreditConsumptionFilter{From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	if len(trend) != 3 || trend[0].Day != "2026-08-01" || trend[0].TotalMicrocredits != 100 || trend[1].Day != "2026-08-02" || trend[1].TotalMicrocredits != 200 || trend[2].Day != "2026-08-03" || trend[2].TotalMicrocredits != 0 {
		t.Fatalf("unexpected boundary trend: %#v", trend)
	}
}

func TestAdminCreditConsumptionVideoSummaryUsesOneDurationPerSettledOrder(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:admin-credit-consumption-video?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.BillingOrder{}, &model.ApiCallLog{}, &model.CreditLedgerEntry{}); err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	settledAt := time.Date(2026, time.August, 21, 10, 0, 0, 0, location)
	orders := []model.BillingOrder{
		{ID: "per-second", UserID: "user-1", IdempotencyKey: "per-second", Capability: "video", BillingMode: "per_second", Quantity: 6, AmountMicrocredits: 600, ActualAmountMicrocredits: 600, Status: model.BillingStatusSettled, SettledAt: timePointer(settledAt)},
		{ID: "fixed", UserID: "user-1", IdempotencyKey: "fixed", Capability: "video", BillingMode: "fixed_request", Quantity: 1, AmountMicrocredits: 1_200, ActualAmountMicrocredits: 1_200, Status: model.BillingStatusSettled, SettledAt: timePointer(settledAt)},
		// Token order refunded part of its reservation: only the final actual debit is counted.
		{ID: "token-refund", UserID: "user-1", IdempotencyKey: "token-refund", Capability: "video", BillingMode: "token", Quantity: 96_030, AmountMicrocredits: 1_000, ReservedAmountMicrocredits: 1_000, ActualAmountMicrocredits: 700, RefundedAmountMicrocredits: 300, UsageAvailable: true, Status: model.BillingStatusSettled, SettledAt: timePointer(settledAt)},
		// Token order exceeded its reservation: the supplemental debit is included.
		{ID: "token-supplement", UserID: "user-1", IdempotencyKey: "token-supplement", Capability: "video", BillingMode: "token", Quantity: 96_030, AmountMicrocredits: 1_000, ReservedAmountMicrocredits: 1_000, ActualAmountMicrocredits: 1_300, UsageAvailable: true, Status: model.BillingStatusSettled, SettledAt: timePointer(settledAt)},
		{ID: "no-duration", UserID: "user-1", IdempotencyKey: "no-duration", Capability: "video", BillingMode: "fixed_request", Quantity: 1, AmountMicrocredits: 500, ActualAmountMicrocredits: 500, Status: model.BillingStatusSettled, SettledAt: timePointer(settledAt)},
		{ID: "zero-charge", UserID: "user-1", IdempotencyKey: "zero-charge", Capability: "video", BillingMode: "per_second", Quantity: 8, AmountMicrocredits: 100, UsageAvailable: true, Status: model.BillingStatusSettled, SettledAt: timePointer(settledAt)},
		{ID: "refunded", UserID: "user-1", IdempotencyKey: "refunded", Capability: "video", BillingMode: "per_second", Quantity: 20, AmountMicrocredits: 9_000, Status: model.BillingStatusRefunded, SettledAt: timePointer(settledAt)},
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatal(err)
	}
	logs := []model.ApiCallLog{
		{ID: "fixed-create", BillingOrderID: "fixed", Capability: "video", Status: model.ApiCallStatusSucceeded, VideoSeconds: 10},
		{ID: "fixed-retry", BillingOrderID: "fixed", Capability: "video", Status: model.ApiCallStatusSucceeded, VideoSeconds: 5},
		{ID: "token-refund-create", BillingOrderID: "token-refund", Capability: "video", Status: model.ApiCallStatusSucceeded, VideoSeconds: 4},
		{ID: "token-refund-poll", BillingOrderID: "token-refund", Capability: "video", Status: model.ApiCallStatusSucceeded, VideoSeconds: 4},
		{ID: "token-supplement-create", BillingOrderID: "token-supplement", Capability: "video", Status: model.ApiCallStatusSucceeded, VideoSeconds: 6},
		{ID: "refunded-create", BillingOrderID: "refunded", Capability: "video", Status: model.ApiCallStatusSucceeded, VideoSeconds: 20},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}
	ledgers := []model.CreditLedgerEntry{
		{ID: "per-second-consume", UserID: "user-1", Type: model.CreditLedgerConsume, AmountMicrocredits: -600, BillingOrderID: "per-second"},
		{ID: "fixed-consume", UserID: "user-1", Type: model.CreditLedgerConsume, AmountMicrocredits: -1_200, BillingOrderID: "fixed"},
		{ID: "token-refund-consume", UserID: "user-1", Type: model.CreditLedgerConsume, AmountMicrocredits: -700, BillingOrderID: "token-refund"},
		{ID: "token-refund-return", UserID: "user-1", Type: model.CreditLedgerRefund, AmountMicrocredits: 300, BillingOrderID: "token-refund"},
		{ID: "token-supplement-consume", UserID: "user-1", Type: model.CreditLedgerConsume, AmountMicrocredits: -1_300, BillingOrderID: "token-supplement"},
		{ID: "no-duration-consume", UserID: "user-1", Type: model.CreditLedgerConsume, AmountMicrocredits: -500, BillingOrderID: "no-duration"},
		{ID: "zero-charge-consume", UserID: "user-1", Type: model.CreditLedgerConsume, AmountMicrocredits: 0, BillingOrderID: "zero-charge"},
		{ID: "refunded-consume", UserID: "user-1", Type: model.CreditLedgerConsume, AmountMicrocredits: -9_000, BillingOrderID: "refunded"},
	}
	if err := db.Create(&ledgers).Error; err != nil {
		t.Fatal(err)
	}

	summary, err := (&Repository{db: db}).AdminCreditConsumptionSummary(CreditConsumptionFilter{
		From: settledAt.Add(-time.Hour),
		To:   settledAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.VideoSeconds != 26 || summary.VideoOrders != 4 || summary.VideoMicrocredits != 3_800 || math.Abs(summary.AvgVideoMicrocreditsPerSecond-146.15384615384616) > 0.000001 {
		t.Fatalf("unexpected video summary: %#v", summary)
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}
