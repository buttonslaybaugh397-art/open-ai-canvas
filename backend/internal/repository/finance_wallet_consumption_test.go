package repository

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreditConsumptionDailyUsesShanghaiDaysAndConsumeRowsOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newRepositoryID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.CreditLedgerEntry{}); err != nil {
		t.Fatal(err)
	}
	entries := []model.CreditLedgerEntry{
		{ID: "consume-before-midnight", UserID: "user-1", Type: model.CreditLedgerConsume, AmountMicrocredits: -1_000_000, CreatedAt: time.Date(2026, 9, 4, 15, 59, 59, 0, time.UTC)},
		{ID: "consume-at-midnight", UserID: "user-1", Type: model.CreditLedgerConsume, AmountMicrocredits: -2_000_000, CreatedAt: time.Date(2026, 9, 4, 16, 0, 0, 0, time.UTC)},
		{ID: "legacy-positive-consume", UserID: "user-1", Type: model.CreditLedgerConsume, AmountMicrocredits: 500_000, CreatedAt: time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)},
		{ID: "refund-excluded", UserID: "user-1", Type: model.CreditLedgerRefund, AmountMicrocredits: 2_000_000, CreatedAt: time.Date(2026, 9, 5, 2, 0, 0, 0, time.UTC)},
		{ID: "other-user-excluded", UserID: "user-2", Type: model.CreditLedgerConsume, AmountMicrocredits: -9_000_000, CreatedAt: time.Date(2026, 9, 5, 3, 0, 0, 0, time.UTC)},
	}
	if err := db.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	start := time.Date(2026, 9, 4, 0, 0, 0, 0, location)
	end := time.Date(2026, 9, 6, 0, 0, 0, 0, location)
	rows, err := New(db).CreditConsumptionDaily("user-1", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("daily rows = %#v, want two Shanghai days", rows)
	}
	if rows[0].Day != "2026-09-04" || rows[0].AmountMicrocredits != 1_000_000 || rows[0].Count != 1 {
		t.Fatalf("first day = %#v", rows[0])
	}
	if rows[1].Day != "2026-09-05" || rows[1].AmountMicrocredits != 2_500_000 || rows[1].Count != 2 {
		t.Fatalf("second day = %#v", rows[1])
	}
}
