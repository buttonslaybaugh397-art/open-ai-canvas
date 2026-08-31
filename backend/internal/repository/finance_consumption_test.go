package repository

import (
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
	if got.Today != 1_200_000 || got.Yesterday != 500_000 || got.Week != 2_700_000 || got.Month != 4_700_000 {
		t.Fatalf("unexpected consumption totals: %#v", got)
	}
}

func TestAdminCreditConsumptionAggregatesUsersModelsAndTrend(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:admin-credit-consumption?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.BillingOrder{}); err != nil {
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
	if len(trend) != 2 || trend[0].Day != "2026-08-20" || trend[0].TotalMicrocredits != 800_000 || trend[1].Day != "2026-08-21" || trend[1].TotalMicrocredits != 2_500_000 {
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

func timePointer(value time.Time) *time.Time {
	return &value
}
