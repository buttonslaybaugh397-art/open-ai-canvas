package service

import (
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

type AdminCreditConsumptionQuery struct {
	From       string
	To         string
	UserID     string
	Model      string
	Capability string
	UserPage   int
	UserLimit  int
	ModelPage  int
	ModelLimit int
}

type AdminCreditConsumptionOverview struct {
	From         time.Time                          `json:"from"`
	To           time.Time                          `json:"to"`
	Summary      AdminCreditConsumptionSummary      `json:"summary"`
	Trend        []AdminCreditConsumptionTrend      `json:"trend"`
	Capabilities []AdminCreditConsumptionCapability `json:"capabilities"`
	Users        AdminCreditConsumptionUserPage     `json:"users"`
	Models       AdminCreditConsumptionModelPage    `json:"models"`
}

type AdminCreditConsumptionSummary struct {
	AllTimeMicrocredits        int64 `json:"allTimeMicrocredits"`
	PeriodMicrocredits         int64 `json:"periodMicrocredits"`
	SettledOrders              int64 `json:"settledOrders"`
	ConsumingUsers             int64 `json:"consumingUsers"`
	UsedModels                 int64 `json:"usedModels"`
	PreviousPeriodMicrocredits int64 `json:"previousPeriodMicrocredits"`
	PreviousSettledOrders      int64 `json:"previousSettledOrders"`
	PreviousConsumingUsers     int64 `json:"previousConsumingUsers"`
	PreviousUsedModels         int64 `json:"previousUsedModels"`
}

type AdminCreditConsumptionTrend struct {
	Day               string `json:"day"`
	TotalMicrocredits int64  `json:"totalMicrocredits"`
	OrderCount        int64  `json:"orderCount"`
	UniqueUsers       int64  `json:"uniqueUsers"`
}

type AdminCreditConsumptionCapability struct {
	Capability        string    `json:"capability"`
	TotalMicrocredits int64     `json:"totalMicrocredits"`
	OrderCount        int64     `json:"orderCount"`
	UniqueUsers       int64     `json:"uniqueUsers"`
	ModelCount        int64     `json:"modelCount"`
	LastConsumedAt    time.Time `json:"lastConsumedAt"`
}

type AdminCreditConsumptionUser struct {
	UserID            string    `json:"userId"`
	Username          string    `json:"username"`
	DisplayName       string    `json:"displayName"`
	Email             string    `json:"email,omitempty"`
	TotalMicrocredits int64     `json:"totalMicrocredits"`
	OrderCount        int64     `json:"orderCount"`
	ModelCount        int64     `json:"modelCount"`
	LastConsumedAt    time.Time `json:"lastConsumedAt"`
}

type AdminCreditConsumptionUserPage struct {
	Items []AdminCreditConsumptionUser `json:"items"`
	Total int64                        `json:"total"`
	Page  int                          `json:"page"`
	Limit int                          `json:"limit"`
}

type AdminCreditConsumptionModel struct {
	Model             string    `json:"model"`
	Capability        string    `json:"capability"`
	TotalMicrocredits int64     `json:"totalMicrocredits"`
	OrderCount        int64     `json:"orderCount"`
	UniqueUsers       int64     `json:"uniqueUsers"`
	LastConsumedAt    time.Time `json:"lastConsumedAt"`
}

type AdminCreditConsumptionModelPage struct {
	Items []AdminCreditConsumptionModel `json:"items"`
	Total int64                         `json:"total"`
	Page  int                           `json:"page"`
	Limit int                           `json:"limit"`
}

func (s *Service) AdminCreditConsumption(actor *model.User, query AdminCreditConsumptionQuery) (*AdminCreditConsumptionOverview, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	filter := normalizeCreditConsumptionFilter(query)
	userPage, userLimit := normalizeAdminPage(query.UserPage, query.UserLimit)
	modelPage, modelLimit := normalizeAdminPage(query.ModelPage, query.ModelLimit)
	summary, err := s.repo.AdminCreditConsumptionSummary(filter)
	if err != nil {
		return nil, err
	}
	trendRows, err := s.repo.AdminCreditConsumptionTrend(filter)
	if err != nil {
		return nil, err
	}
	capabilityRows, err := s.repo.AdminCreditConsumptionCapabilities(filter)
	if err != nil {
		return nil, err
	}
	userRows, userTotal, err := s.repo.AdminCreditConsumptionUsers(filter, userLimit, (userPage-1)*userLimit)
	if err != nil {
		return nil, err
	}
	modelRows, modelTotal, err := s.repo.AdminCreditConsumptionModels(filter, modelLimit, (modelPage-1)*modelLimit)
	if err != nil {
		return nil, err
	}
	result := &AdminCreditConsumptionOverview{
		From: filter.From,
		To:   filter.To,
		Summary: AdminCreditConsumptionSummary{
			AllTimeMicrocredits: summary.AllTimeMicrocredits, PeriodMicrocredits: summary.PeriodMicrocredits,
			SettledOrders: summary.SettledOrders, ConsumingUsers: summary.ConsumingUsers, UsedModels: summary.UsedModels,
			PreviousPeriodMicrocredits: summary.PreviousPeriodMicrocredits, PreviousSettledOrders: summary.PreviousSettledOrders,
			PreviousConsumingUsers: summary.PreviousConsumingUsers, PreviousUsedModels: summary.PreviousUsedModels,
		},
		Trend: make([]AdminCreditConsumptionTrend, 0, len(trendRows)), Capabilities: make([]AdminCreditConsumptionCapability, 0, len(capabilityRows)),
		Users:  AdminCreditConsumptionUserPage{Items: make([]AdminCreditConsumptionUser, 0, len(userRows)), Total: userTotal, Page: userPage, Limit: userLimit},
		Models: AdminCreditConsumptionModelPage{Items: make([]AdminCreditConsumptionModel, 0, len(modelRows)), Total: modelTotal, Page: modelPage, Limit: modelLimit},
	}
	for _, row := range trendRows {
		result.Trend = append(result.Trend, AdminCreditConsumptionTrend(row))
	}
	for _, row := range capabilityRows {
		result.Capabilities = append(result.Capabilities, AdminCreditConsumptionCapability{
			Capability: row.Capability, TotalMicrocredits: row.TotalMicrocredits, OrderCount: row.OrderCount,
			UniqueUsers: row.UniqueUsers, ModelCount: row.ModelCount, LastConsumedAt: creditConsumptionTimeFromUnix(row.LastConsumedUnix),
		})
	}
	for _, row := range userRows {
		result.Users.Items = append(result.Users.Items, AdminCreditConsumptionUser{
			UserID: row.UserID, Username: row.Username, DisplayName: row.DisplayName, Email: row.Email,
			TotalMicrocredits: row.TotalMicrocredits, OrderCount: row.OrderCount, ModelCount: row.ModelCount,
			LastConsumedAt: creditConsumptionTimeFromUnix(row.LastConsumedUnix),
		})
	}
	for _, row := range modelRows {
		result.Models.Items = append(result.Models.Items, AdminCreditConsumptionModel{
			Model: row.Model, Capability: row.Capability, TotalMicrocredits: row.TotalMicrocredits,
			OrderCount: row.OrderCount, UniqueUsers: row.UniqueUsers,
			LastConsumedAt: creditConsumptionTimeFromUnix(row.LastConsumedUnix),
		})
	}
	return result, nil
}

func normalizeCreditConsumptionFilter(query AdminCreditConsumptionQuery) repository.CreditConsumptionFilter {
	now := time.Now().In(creditConsumptionLocation)
	return normalizeCreditConsumptionFilterAt(query, now)
}

func normalizeCreditConsumptionFilterAt(query AdminCreditConsumptionQuery, now time.Time) repository.CreditConsumptionFilter {
	now = now.In(creditConsumptionLocation)
	to := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, creditConsumptionLocation)
	from := to.AddDate(0, 0, -30)
	if parsed, ok := parseCreditConsumptionTime(query.From); ok {
		from = parsed
	}
	if parsed, ok := parseCreditConsumptionTime(query.To); ok {
		to = parsed
		if len(strings.TrimSpace(query.To)) == len("2006-01-02") {
			to = to.AddDate(0, 0, 1)
		}
	}
	// A date-only upper bound for today means "up to now", not the rest of the day.
	// This prevents future timestamps from entering live daily statistics.
	if to.After(now) {
		to = now
	}
	if !to.After(from) {
		if from.After(now) {
			return repository.CreditConsumptionFilter{
				From:       now,
				To:         now,
				UserID:     strings.TrimSpace(query.UserID),
				Model:      strings.TrimSpace(query.Model),
				Capability: normalizeCapability(query.Capability),
			}
		}
		to = from.AddDate(0, 0, 1)
		if to.After(now) {
			to = now
		}
		if !to.After(from) {
			to = from
		}
	}
	if to.Sub(from) > 366*24*time.Hour {
		from = to.AddDate(-1, 0, 0)
	}
	return repository.CreditConsumptionFilter{
		From:       from,
		To:         to,
		UserID:     strings.TrimSpace(query.UserID),
		Model:      strings.TrimSpace(query.Model),
		Capability: normalizeCapability(query.Capability),
	}
}

func parseCreditConsumptionTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.In(creditConsumptionLocation), true
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, creditConsumptionLocation); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func creditConsumptionTimeFromUnix(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(value, 0).In(creditConsumptionLocation)
}
