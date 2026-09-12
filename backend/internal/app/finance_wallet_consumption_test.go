package app

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBuildWalletConsumptionSummaryUsesNaturalPeriodBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 9, 15, 30, 0, 0, walletConsumptionLocation)
	rows := []repository.CreditConsumptionDay{
		{Day: "2026-08-31", AmountMicrocredits: 99_000_000, Count: 1},
		{Day: "2026-09-01", AmountMicrocredits: 1_000_000, Count: 1},
		{Day: "2026-09-06", AmountMicrocredits: 6_000_000, Count: 2},
		{Day: "2026-09-07", AmountMicrocredits: 7_000_000, Count: 3},
		{Day: "2026-09-08", AmountMicrocredits: 8_000_000, Count: 4},
		{Day: "2026-09-09", AmountMicrocredits: 9_000_000, Count: 5},
	}
	summary := buildWalletConsumptionSummary(now, rows)
	if summary.TodayMicrocredits != 9_000_000 {
		t.Fatalf("today = %d", summary.TodayMicrocredits)
	}
	if summary.YesterdayMicrocredits != 8_000_000 {
		t.Fatalf("yesterday = %d", summary.YesterdayMicrocredits)
	}
	if summary.WeekMicrocredits != 24_000_000 {
		t.Fatalf("week = %d, want Monday through today", summary.WeekMicrocredits)
	}
	if summary.MonthMicrocredits != 31_000_000 {
		t.Fatalf("month = %d, want September only", summary.MonthMicrocredits)
	}
	if len(summary.Daily) != 9 || summary.Daily[1].Day != "2026-09-02" || summary.Daily[1].AmountMicrocredits != 0 {
		t.Fatalf("daily series = %#v", summary.Daily)
	}
}

func TestWalletConsumptionIsIndependentFromLedgerFilterAndPagination(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:wallet-consumption-service?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.CreditAccount{}, &model.CreditLedgerEntry{}); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "user-1"}
	other := "user-2"
	now := time.Now().In(walletConsumptionLocation)
	entries := []model.CreditLedgerEntry{
		{ID: "income-1", UserID: user.ID, Type: model.CreditLedgerRedeem, AmountMicrocredits: 20_000_000, CreatedAt: now},
		{ID: "consume-1", UserID: user.ID, Type: model.CreditLedgerConsume, AmountMicrocredits: -2_000_000, CreatedAt: now},
		{ID: "consume-2", UserID: user.ID, Type: model.CreditLedgerConsume, AmountMicrocredits: -3_000_000, CreatedAt: now},
		{ID: "other-user", UserID: other, Type: model.CreditLedgerConsume, AmountMicrocredits: -99_000_000, CreatedAt: now},
	}
	if err := db.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db), dataDir: t.TempDir()}
	wallet, err := svc.Wallet(user, "income", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(wallet.Entries) != 1 || wallet.Entries[0].Type != model.CreditLedgerRedeem {
		t.Fatalf("filtered ledger = %#v", wallet.Entries)
	}
	if wallet.Consumption.TodayMicrocredits != 5_000_000 || wallet.Consumption.MonthMicrocredits != 5_000_000 {
		t.Fatalf("consumption = %#v, want all current-user consume rows", wallet.Consumption)
	}
}
