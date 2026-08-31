package service

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWalletIncludesSettledCreditConsumption(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.SystemSetting{},
		&model.CreditAccount{},
		&model.CreditLedgerEntry{},
		&model.BillingOrder{},
	); err != nil {
		t.Fatal(err)
	}

	now := time.Now().In(creditConsumptionLocation)
	settledAt := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, creditConsumptionLocation)
	order := model.BillingOrder{
		ID: "wallet-consumption", UserID: "user-1", IdempotencyKey: "wallet-consumption",
		AmountMicrocredits: 1_500_000, ActualAmountMicrocredits: 1_250_000,
		Status: model.BillingStatusSettled, SettledAt: &settledAt,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}

	summary, err := (&Service{repo: repository.New(db)}).Wallet(&model.User{ID: "user-1"}, "all", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Consumption.TodayMicrocredits != 1_250_000 ||
		summary.Consumption.WeekMicrocredits != 1_250_000 ||
		summary.Consumption.MonthMicrocredits != 1_250_000 {
		t.Fatalf("Wallet() consumption = %#v", summary.Consumption)
	}
}
