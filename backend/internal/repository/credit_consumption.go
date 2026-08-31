package repository

import (
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type CreditConsumptionFilter struct {
	From       time.Time
	To         time.Time
	UserID     string
	Model      string
	Capability string
}

type AdminCreditConsumptionSummary struct {
	AllTimeMicrocredits        int64
	PeriodMicrocredits         int64
	SettledOrders              int64
	ConsumingUsers             int64
	UsedModels                 int64
	PreviousPeriodMicrocredits int64
	PreviousSettledOrders      int64
	PreviousConsumingUsers     int64
	PreviousUsedModels         int64
}

type AdminCreditConsumptionTrendRow struct {
	Day               string
	TotalMicrocredits int64
	OrderCount        int64
	UniqueUsers       int64
}

type AdminCreditConsumptionUserRow struct {
	UserID            string
	Username          string
	DisplayName       string
	Email             string
	TotalMicrocredits int64
	OrderCount        int64
	ModelCount        int64
	LastConsumedUnix  int64
}

type AdminCreditConsumptionModelRow struct {
	Model             string
	Capability        string
	TotalMicrocredits int64
	OrderCount        int64
	UniqueUsers       int64
	LastConsumedUnix  int64
}

type AdminCreditConsumptionCapabilityRow struct {
	Capability        string
	TotalMicrocredits int64
	OrderCount        int64
	UniqueUsers       int64
	ModelCount        int64
	LastConsumedUnix  int64
}

func (r *Repository) AdminCreditConsumptionSummary(filter CreditConsumptionFilter) (AdminCreditConsumptionSummary, error) {
	var summary AdminCreditConsumptionSummary
	amountExpression := billingConsumptionAmountExpression()
	periodSelect := "COALESCE(SUM(" + amountExpression + "), 0) AS period_microcredits, " +
		"COUNT(*) AS settled_orders, " +
		"COUNT(DISTINCT billing_orders.user_id) AS consuming_users, " +
		"COUNT(DISTINCT billing_orders.model) AS used_models"
	if err := r.creditConsumptionQuery(filter, true).Select(periodSelect).Scan(&summary).Error; err != nil {
		return summary, err
	}
	previousFilter := filter
	previousFilter.To = filter.From
	previousFilter.From = filter.From.Add(-filter.To.Sub(filter.From))
	var previous struct {
		PeriodMicrocredits int64 `gorm:"column:period_microcredits"`
		SettledOrders      int64 `gorm:"column:settled_orders"`
		ConsumingUsers     int64 `gorm:"column:consuming_users"`
		UsedModels         int64 `gorm:"column:used_models"`
	}
	if err := r.creditConsumptionQuery(previousFilter, true).Select(periodSelect).Scan(&previous).Error; err != nil {
		return summary, err
	}
	summary.PreviousPeriodMicrocredits = previous.PeriodMicrocredits
	summary.PreviousSettledOrders = previous.SettledOrders
	summary.PreviousConsumingUsers = previous.ConsumingUsers
	summary.PreviousUsedModels = previous.UsedModels
	var allTime struct {
		Total int64 `gorm:"column:total"`
	}
	if err := r.creditConsumptionQuery(filter, false).
		Select("COALESCE(SUM(" + amountExpression + "), 0) AS total").
		Scan(&allTime).Error; err != nil {
		return summary, err
	}
	summary.AllTimeMicrocredits = allTime.Total
	return summary, nil
}

func (r *Repository) AdminCreditConsumptionCapabilities(filter CreditConsumptionFilter) ([]AdminCreditConsumptionCapabilityRow, error) {
	rows := make([]AdminCreditConsumptionCapabilityRow, 0)
	err := r.creditConsumptionQuery(filter, true).
		Select(
			"billing_orders.capability, " +
				"COALESCE(SUM(" + billingConsumptionAmountExpression() + "), 0) AS total_microcredits, " +
				"COUNT(*) AS order_count, " +
				"COUNT(DISTINCT billing_orders.user_id) AS unique_users, " +
				"COUNT(DISTINCT billing_orders.model) AS model_count, " +
				"MAX(" + billingConsumptionUnixTimeExpression(r) + ") AS last_consumed_unix",
		).
		Group("billing_orders.capability").
		Order("total_microcredits desc, last_consumed_unix desc").
		Scan(&rows).Error
	return rows, err
}

func (r *Repository) AdminCreditConsumptionTrend(filter CreditConsumptionFilter) ([]AdminCreditConsumptionTrendRow, error) {
	rows := make([]AdminCreditConsumptionTrendRow, 0)
	dayExpression := "TO_CHAR(" + billingConsumptionTimeExpression() + " AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD')"
	if r.db.Dialector.Name() == "sqlite" {
		dayExpression = "strftime('%Y-%m-%d', " + billingConsumptionTimeExpression() + ", '+8 hours')"
	}
	err := r.creditConsumptionQuery(filter, true).
		Select(
			dayExpression + " AS day, " +
				"COALESCE(SUM(" + billingConsumptionAmountExpression() + "), 0) AS total_microcredits, " +
				"COUNT(*) AS order_count, " +
				"COUNT(DISTINCT billing_orders.user_id) AS unique_users",
		).
		Group(dayExpression).
		Order("day asc").
		Scan(&rows).Error
	return rows, err
}

func (r *Repository) AdminCreditConsumptionUsers(filter CreditConsumptionFilter, limit int, offset int) ([]AdminCreditConsumptionUserRow, int64, error) {
	var total int64
	if err := r.creditConsumptionQuery(filter, true).
		Distinct("billing_orders.user_id").
		Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]AdminCreditConsumptionUserRow, 0)
	err := r.creditConsumptionQuery(filter, true).
		Joins("LEFT JOIN users ON users.id = billing_orders.user_id").
		Select(
			"billing_orders.user_id, users.username, users.display_name, users.email, " +
				"COALESCE(SUM(" + billingConsumptionAmountExpression() + "), 0) AS total_microcredits, " +
				"COUNT(*) AS order_count, " +
				"COUNT(DISTINCT billing_orders.model) AS model_count, " +
				"MAX(" + billingConsumptionUnixTimeExpression(r) + ") AS last_consumed_unix",
		).
		Group("billing_orders.user_id, users.username, users.display_name, users.email").
		Order("total_microcredits desc, last_consumed_unix desc").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error
	return rows, total, err
}

func (r *Repository) AdminCreditConsumptionModels(filter CreditConsumptionFilter, limit int, offset int) ([]AdminCreditConsumptionModelRow, int64, error) {
	grouped := r.creditConsumptionQuery(filter, true).
		Select("billing_orders.model, billing_orders.capability").
		Group("billing_orders.model, billing_orders.capability")
	var total int64
	if err := r.db.Table("(?) AS credit_consumption_models", grouped).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]AdminCreditConsumptionModelRow, 0)
	err := r.creditConsumptionQuery(filter, true).
		Select(
			"billing_orders.model, billing_orders.capability, " +
				"COALESCE(SUM(" + billingConsumptionAmountExpression() + "), 0) AS total_microcredits, " +
				"COUNT(*) AS order_count, " +
				"COUNT(DISTINCT billing_orders.user_id) AS unique_users, " +
				"MAX(" + billingConsumptionUnixTimeExpression(r) + ") AS last_consumed_unix",
		).
		Group("billing_orders.model, billing_orders.capability").
		Order("total_microcredits desc, last_consumed_unix desc").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error
	return rows, total, err
}

func (r *Repository) creditConsumptionQuery(filter CreditConsumptionFilter, includeRange bool) *gorm.DB {
	query := r.db.Model(&model.BillingOrder{}).
		Where("billing_orders.status = ?", model.BillingStatusSettled).
		Where("(billing_orders.actual_amount_microcredits > 0 OR billing_orders.amount_microcredits > 0)")
	if includeRange {
		comparisonExpression := billingConsumptionTimeExpression()
		parameterExpression := "?"
		if r.db.Dialector.Name() == "sqlite" {
			comparisonExpression = "julianday(" + comparisonExpression + ")"
			parameterExpression = "julianday(?)"
		}
		query = query.Where(comparisonExpression+" >= "+parameterExpression+" AND "+comparisonExpression+" < "+parameterExpression, filter.From, filter.To)
	}
	if filter.UserID != "" {
		query = query.Where("billing_orders.user_id = ?", filter.UserID)
	}
	if filter.Model != "" {
		query = query.Where("billing_orders.model = ?", filter.Model)
	}
	if filter.Capability != "" {
		query = query.Where("billing_orders.capability = ?", filter.Capability)
	}
	return query
}

func billingConsumptionAmountExpression() string {
	return "CASE WHEN billing_orders.actual_amount_microcredits > 0 THEN billing_orders.actual_amount_microcredits ELSE billing_orders.amount_microcredits END"
}

func billingConsumptionTimeExpression() string {
	return "COALESCE(billing_orders.settled_at, billing_orders.updated_at, billing_orders.created_at)"
}

func billingConsumptionUnixTimeExpression(r *Repository) string {
	timestampExpression := billingConsumptionTimeExpression()
	if r.db.Dialector.Name() == "sqlite" {
		return "unixepoch(" + timestampExpression + ")"
	}
	return "EXTRACT(EPOCH FROM " + timestampExpression + " )::bigint"
}
