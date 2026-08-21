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

func timePointer(value time.Time) *time.Time {
	return &value
}
