package app

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type AnalyticsQuery struct {
	From       string
	To         string
	UserID     string
	Model      string
	ChannelID  string
	Capability string
}

type AnalyticsOverview struct {
	From     time.Time             `json:"from"`
	To       time.Time             `json:"to"`
	KPI      AnalyticsKPI          `json:"kpi"`
	Trend    []AnalyticsTrendPoint `json:"trend"`
	Models   []AnalyticsModelRow   `json:"models"`
	Channels []AnalyticsChannelRow `json:"channels"`
	Users    []AnalyticsUserRow    `json:"users"`
	Failures []AnalyticsFailureRow `json:"failures"`
}

type AnalyticsKPI struct {
	ActiveUsers                 int     `json:"activeUsers"`
	DAU                         int     `json:"dau"`
	WAU                         int     `json:"wau"`
	MAU                         int     `json:"mau"`
	GenerationTasks             int     `json:"generationTasks"`
	TextTasks                   int     `json:"textTasks"`
	ImageTasks                  int     `json:"imageTasks"`
	VideoTasks                  int     `json:"videoTasks"`
	AudioTasks                  int     `json:"audioTasks"`
	SucceededTasks              int     `json:"succeededTasks"`
	FailedTasks                 int     `json:"failedTasks"`
	CancelledTasks              int     `json:"cancelledTasks"`
	QueuedTasks                 int     `json:"queuedTasks"`
	RunningTasks                int     `json:"runningTasks"`
	TaskSuccessRate             float64 `json:"taskSuccessRate"`
	MediaCount                  int     `json:"mediaCount"`
	GeneratedImages             int     `json:"generatedImages"`
	GeneratedVideos             int     `json:"generatedVideos"`
	GeneratedAudio              int     `json:"generatedAudio"`
	VideoSeconds                int     `json:"videoSeconds"`
	AverageTaskDurationMs       int64   `json:"averageTaskDurationMs"`
	UpstreamRequests            int     `json:"upstreamRequests"`
	SucceededRequests           int     `json:"succeededRequests"`
	FailedRequests              int     `json:"failedRequests"`
	SuccessRate                 float64 `json:"successRate"`
	P95DurationMs               int64   `json:"p95DurationMs"`
	InputTokens                 int64   `json:"inputTokens"`
	OutputTokens                int64   `json:"outputTokens"`
	CachedTokens                int64   `json:"cachedTokens"`
	UsageAvailable              bool    `json:"usageAvailable"`
	CreditsConsumedMicrocredits int64   `json:"creditsConsumedMicrocredits"`
	CurrentQueuedTasks          int64   `json:"currentQueuedTasks"`
	EstimatedCostMicros         int64   `json:"estimatedCostMicros"`
	CostAvailable               bool    `json:"costAvailable"`
	Currency                    string  `json:"currency"`
}

type AnalyticsTrendPoint struct {
	Day                string  `json:"day"`
	Tasks              int     `json:"tasks"`
	TextTasks          int     `json:"textTasks"`
	ImageTasks         int     `json:"imageTasks"`
	VideoTasks         int     `json:"videoTasks"`
	AudioTasks         int     `json:"audioTasks"`
	SucceededTasks     int     `json:"succeededTasks"`
	FailedTasks        int     `json:"failedTasks"`
	TaskSuccessRate    float64 `json:"taskSuccessRate"`
	MediaCount         int     `json:"mediaCount"`
	VideoSeconds       int     `json:"videoSeconds"`
	Requests           int     `json:"requests"`
	ActiveUsers        int     `json:"activeUsers"`
	RequestSuccessRate float64 `json:"requestSuccessRate"`
}

type AnalyticsModelRow struct {
	Model                       string  `json:"model"`
	Capability                  string  `json:"capability"`
	Tasks                       int     `json:"tasks"`
	SucceededTasks              int     `json:"succeededTasks"`
	FailedTasks                 int     `json:"failedTasks"`
	CancelledTasks              int     `json:"cancelledTasks"`
	QueuedTasks                 int     `json:"queuedTasks"`
	RunningTasks                int     `json:"runningTasks"`
	Requests                    int     `json:"requests"`
	SucceededRequests           int     `json:"succeededRequests"`
	FailedRequests              int     `json:"failedRequests"`
	UniqueUsers                 int     `json:"uniqueUsers"`
	TaskSuccessRate             float64 `json:"taskSuccessRate"`
	RequestSuccessRate          float64 `json:"requestSuccessRate"`
	P50DurationMs               int64   `json:"p50DurationMs"`
	P95DurationMs               int64   `json:"p95DurationMs"`
	InputTokens                 int64   `json:"inputTokens"`
	OutputTokens                int64   `json:"outputTokens"`
	CachedTokens                int64   `json:"cachedTokens"`
	UsageAvailable              bool    `json:"usageAvailable"`
	MediaCount                  int     `json:"mediaCount"`
	GeneratedImages             int     `json:"generatedImages"`
	GeneratedVideos             int     `json:"generatedVideos"`
	GeneratedAudio              int     `json:"generatedAudio"`
	VideoSeconds                int     `json:"videoSeconds"`
	CreditsConsumedMicrocredits int64   `json:"creditsConsumedMicrocredits"`
	EstimatedCostMicros         int64   `json:"estimatedCostMicros"`
	CostAvailable               bool    `json:"costAvailable"`
	Currency                    string  `json:"currency"`
}

type AnalyticsUserRow struct {
	UserID                      string                    `json:"userId"`
	Name                        string                    `json:"name"`
	ActiveDays                  int                       `json:"activeDays"`
	LoginCount                  int                       `json:"loginCount"`
	FirstActiveAt               *time.Time                `json:"firstActiveAt,omitempty"`
	LastActiveAt                *time.Time                `json:"lastActiveAt,omitempty"`
	Tasks                       int                       `json:"tasks"`
	TextTasks                   int                       `json:"textTasks"`
	ImageTasks                  int                       `json:"imageTasks"`
	VideoTasks                  int                       `json:"videoTasks"`
	AudioTasks                  int                       `json:"audioTasks"`
	SucceededTasks              int                       `json:"succeededTasks"`
	FailedTasks                 int                       `json:"failedTasks"`
	CancelledTasks              int                       `json:"cancelledTasks"`
	QueuedTasks                 int                       `json:"queuedTasks"`
	RunningTasks                int                       `json:"runningTasks"`
	TaskSuccessRate             float64                   `json:"taskSuccessRate"`
	AverageTaskDurationMs       int64                     `json:"averageTaskDurationMs"`
	P95TaskDurationMs           int64                     `json:"p95TaskDurationMs"`
	Requests                    int                       `json:"requests"`
	SucceededRequests           int                       `json:"succeededRequests"`
	FailedRequests              int                       `json:"failedRequests"`
	RequestSuccessRate          float64                   `json:"requestSuccessRate"`
	P95RequestDurationMs        int64                     `json:"p95RequestDurationMs"`
	MediaCount                  int                       `json:"mediaCount"`
	GeneratedImages             int                       `json:"generatedImages"`
	GeneratedVideos             int                       `json:"generatedVideos"`
	GeneratedAudio              int                       `json:"generatedAudio"`
	VideoSeconds                int                       `json:"videoSeconds"`
	InputTokens                 int64                     `json:"inputTokens"`
	OutputTokens                int64                     `json:"outputTokens"`
	CachedTokens                int64                     `json:"cachedTokens"`
	UsageAvailable              bool                      `json:"usageAvailable"`
	CreditsConsumedMicrocredits int64                     `json:"creditsConsumedMicrocredits"`
	EstimatedCostMicros         int64                     `json:"estimatedCostMicros"`
	CostAvailable               bool                      `json:"costAvailable"`
	Currency                    string                    `json:"currency"`
	AgentMessages               int                       `json:"agentMessages"`
	CanvasDays                  int                       `json:"canvasDays"`
	Assets                      int                       `json:"assets"`
	Resources                   int                       `json:"resources"`
	CommonModel                 string                    `json:"commonModel"`
	Models                      []AnalyticsUserModelRow   `json:"models"`
	Channels                    []AnalyticsUserChannelRow `json:"channels"`
	Daily                       []AnalyticsUserDayRow     `json:"daily"`
}

type AnalyticsChannelRow struct {
	ChannelID                   string  `json:"channelId"`
	Name                        string  `json:"name"`
	Tasks                       int     `json:"tasks"`
	Requests                    int     `json:"requests"`
	SucceededRequests           int     `json:"succeededRequests"`
	FailedRequests              int     `json:"failedRequests"`
	RequestSuccessRate          float64 `json:"requestSuccessRate"`
	UniqueUsers                 int     `json:"uniqueUsers"`
	UniqueModels                int     `json:"uniqueModels"`
	P50DurationMs               int64   `json:"p50DurationMs"`
	P95DurationMs               int64   `json:"p95DurationMs"`
	MediaCount                  int     `json:"mediaCount"`
	GeneratedImages             int     `json:"generatedImages"`
	GeneratedVideos             int     `json:"generatedVideos"`
	GeneratedAudio              int     `json:"generatedAudio"`
	VideoSeconds                int     `json:"videoSeconds"`
	InputTokens                 int64   `json:"inputTokens"`
	OutputTokens                int64   `json:"outputTokens"`
	CachedTokens                int64   `json:"cachedTokens"`
	UsageAvailable              bool    `json:"usageAvailable"`
	CreditsConsumedMicrocredits int64   `json:"creditsConsumedMicrocredits"`
	EstimatedCostMicros         int64   `json:"estimatedCostMicros"`
	CostAvailable               bool    `json:"costAvailable"`
	Currency                    string  `json:"currency"`
}

type AnalyticsUserModelRow struct {
	Model                       string  `json:"model"`
	Capability                  string  `json:"capability"`
	Tasks                       int     `json:"tasks"`
	SucceededTasks              int     `json:"succeededTasks"`
	FailedTasks                 int     `json:"failedTasks"`
	Requests                    int     `json:"requests"`
	SucceededRequests           int     `json:"succeededRequests"`
	FailedRequests              int     `json:"failedRequests"`
	TaskSuccessRate             float64 `json:"taskSuccessRate"`
	RequestSuccessRate          float64 `json:"requestSuccessRate"`
	MediaCount                  int     `json:"mediaCount"`
	VideoSeconds                int     `json:"videoSeconds"`
	InputTokens                 int64   `json:"inputTokens"`
	OutputTokens                int64   `json:"outputTokens"`
	CachedTokens                int64   `json:"cachedTokens"`
	UsageAvailable              bool    `json:"usageAvailable"`
	CreditsConsumedMicrocredits int64   `json:"creditsConsumedMicrocredits"`
	EstimatedCostMicros         int64   `json:"estimatedCostMicros"`
	CostAvailable               bool    `json:"costAvailable"`
	Currency                    string  `json:"currency"`
}

type AnalyticsUserChannelRow struct {
	ChannelID                   string  `json:"channelId"`
	Name                        string  `json:"name"`
	Tasks                       int     `json:"tasks"`
	Requests                    int     `json:"requests"`
	SucceededRequests           int     `json:"succeededRequests"`
	FailedRequests              int     `json:"failedRequests"`
	RequestSuccessRate          float64 `json:"requestSuccessRate"`
	UniqueModels                int     `json:"uniqueModels"`
	MediaCount                  int     `json:"mediaCount"`
	GeneratedImages             int     `json:"generatedImages"`
	GeneratedVideos             int     `json:"generatedVideos"`
	GeneratedAudio              int     `json:"generatedAudio"`
	VideoSeconds                int     `json:"videoSeconds"`
	InputTokens                 int64   `json:"inputTokens"`
	OutputTokens                int64   `json:"outputTokens"`
	CachedTokens                int64   `json:"cachedTokens"`
	UsageAvailable              bool    `json:"usageAvailable"`
	CreditsConsumedMicrocredits int64   `json:"creditsConsumedMicrocredits"`
	EstimatedCostMicros         int64   `json:"estimatedCostMicros"`
	CostAvailable               bool    `json:"costAvailable"`
	Currency                    string  `json:"currency"`
}

type AnalyticsUserDayRow struct {
	Day                         string `json:"day"`
	LoginCount                  int    `json:"loginCount"`
	Tasks                       int    `json:"tasks"`
	TextTasks                   int    `json:"textTasks"`
	ImageTasks                  int    `json:"imageTasks"`
	VideoTasks                  int    `json:"videoTasks"`
	AudioTasks                  int    `json:"audioTasks"`
	SucceededTasks              int    `json:"succeededTasks"`
	FailedTasks                 int    `json:"failedTasks"`
	CancelledTasks              int    `json:"cancelledTasks"`
	QueuedTasks                 int    `json:"queuedTasks"`
	RunningTasks                int    `json:"runningTasks"`
	Requests                    int    `json:"requests"`
	SucceededRequests           int    `json:"succeededRequests"`
	FailedRequests              int    `json:"failedRequests"`
	MediaCount                  int    `json:"mediaCount"`
	GeneratedImages             int    `json:"generatedImages"`
	GeneratedVideos             int    `json:"generatedVideos"`
	GeneratedAudio              int    `json:"generatedAudio"`
	VideoSeconds                int    `json:"videoSeconds"`
	InputTokens                 int64  `json:"inputTokens"`
	OutputTokens                int64  `json:"outputTokens"`
	CachedTokens                int64  `json:"cachedTokens"`
	CreditsConsumedMicrocredits int64  `json:"creditsConsumedMicrocredits"`
	EstimatedCostMicros         int64  `json:"estimatedCostMicros"`
	CostAvailable               bool   `json:"costAvailable"`
	Currency                    string `json:"currency"`
	AgentMessages               int    `json:"agentMessages"`
	CanvasActive                bool   `json:"canvasActive"`
	Assets                      int    `json:"assets"`
	Resources                   int    `json:"resources"`
}

type AnalyticsFailureRow struct {
	Type       string    `json:"type"`
	Model      string    `json:"model"`
	Count      int       `json:"count"`
	LastError  string    `json:"lastError"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type APICallLogQuery struct {
	AnalyticsQuery
	RecordType string
	Keyword    string
	Status     string
	IDs        []string
	Page       int
	Limit      int
}

type APICallLogPage struct {
	Logs  []model.ApiCallLog `json:"logs"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
	Limit int                `json:"pageSize"`
}

type ModelPricingRequest struct {
	ChannelID              string `json:"channelId"`
	Model                  string `json:"model"`
	Capability             string `json:"capability"`
	Currency               string `json:"currency"`
	InputPerMillionMicros  int64  `json:"inputPerMillionMicros"`
	OutputPerMillionMicros int64  `json:"outputPerMillionMicros"`
	CachedPerMillionMicros int64  `json:"cachedPerMillionMicros"`
	PerRequestMicros       int64  `json:"perRequestMicros"`
	PerMediaMicros         int64  `json:"perMediaMicros"`
	PerVideoSecondMicros   int64  `json:"perVideoSecondMicros"`
}

func (s *Service) AdminAnalytics(actor *model.User, query AnalyticsQuery) (*AnalyticsOverview, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	filter := normalizeAnalyticsFilter(query)
	tasks, logs, billingOrders, err := s.analyticsFacts(filter)
	if err != nil {
		return nil, err
	}
	activityFilter := filter
	rollingFrom := filter.To.AddDate(0, 0, -30)
	if rollingFrom.Before(activityFilter.From) {
		activityFilter.From = rollingFrom
	}
	activities, err := s.repo.AnalyticsActivities(activityFilter)
	if err != nil {
		return nil, err
	}
	rollingTasks := tasks
	rollingLogs := logs
	if activityFilter.From.Before(filter.From) {
		rollingTasks, rollingLogs, _, err = s.analyticsFacts(activityFilter)
		if err != nil {
			return nil, err
		}
	}
	users, err := s.repo.Users()
	if err != nil {
		return nil, err
	}
	queued, err := s.repo.CurrentQueuedTaskCount()
	if err != nil {
		return nil, err
	}
	result := buildAnalyticsOverview(filter, tasks, rollingTasks, rollingLogs, logs, billingOrders, activities, users)
	channels, err := s.repo.HistoricalSystemChannelReferences()
	if err != nil {
		return nil, err
	}
	channelNames := make(map[string]string, len(channels))
	for _, channel := range channels {
		channelNames[channel.ID] = channel.Name
	}
	for index := range result.Channels {
		result.Channels[index].Name = firstNonEmpty(channelNames[result.Channels[index].ChannelID], result.Channels[index].Name)
	}
	for userIndex := range result.Users {
		for channelIndex := range result.Users[userIndex].Channels {
			channel := &result.Users[userIndex].Channels[channelIndex]
			channel.Name = firstNonEmpty(channelNames[channel.ChannelID], channel.Name)
		}
	}
	result.KPI.CurrentQueuedTasks = queued
	return result, nil
}

func (s *Service) analyticsFacts(filter repository.AnalyticsFilter) ([]model.Task, []model.ApiCallLog, []model.BillingOrder, error) {
	baseFilter := filter
	baseFilter.Model = ""
	baseFilter.ChannelID = ""
	baseFilter.Capability = ""
	tasks, err := s.repo.AnalyticsTasks(baseFilter)
	if err != nil {
		return nil, nil, nil, err
	}
	logs, err := s.repo.AnalyticsAPICallLogs(baseFilter)
	if err != nil {
		return nil, nil, nil, err
	}
	billingOrders, err := s.repo.AnalyticsBillingOrders(baseFilter)
	if err != nil {
		return nil, nil, nil, err
	}
	taskIDs := make([]string, 0, len(logs)+len(billingOrders))
	seenTaskIDs := make(map[string]bool, len(tasks)+len(logs)+len(billingOrders))
	for _, task := range tasks {
		seenTaskIDs[task.ID] = true
	}
	for _, log := range logs {
		if log.TaskID != "" && !seenTaskIDs[log.TaskID] {
			seenTaskIDs[log.TaskID] = true
			taskIDs = append(taskIDs, log.TaskID)
		}
	}
	for _, order := range billingOrders {
		if order.TaskID != "" && !seenTaskIDs[order.TaskID] {
			seenTaskIDs[order.TaskID] = true
			taskIDs = append(taskIDs, order.TaskID)
		}
	}
	taskReferences := append([]model.Task(nil), tasks...)
	if len(taskIDs) > 0 {
		relatedTasks, queryErr := s.repo.APICallLogTasks(taskIDs)
		if queryErr != nil {
			return nil, nil, nil, queryErr
		}
		taskReferences = append(taskReferences, relatedTasks...)
	}
	return filterAnalyticsFacts(filter, tasks, taskReferences, logs, billingOrders)
}

func (s *Service) AdminAPICallLogs(actor *model.User, query APICallLogQuery) (*APICallLogPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if query.RecordType != "" && query.RecordType != "request" && query.RecordType != "download" && query.RecordType != "all" {
		return nil, BadAuthRequest("请求明细类型无效")
	}
	filter := normalizeAnalyticsFilter(query.AnalyticsQuery)
	logs, total, err := s.repo.QueryAPICallLogs(repository.APICallLogFilter{AnalyticsFilter: filter, RecordType: query.RecordType, Keyword: query.Keyword, Status: query.Status, Page: query.Page, Limit: query.Limit})
	if err != nil {
		return nil, err
	}
	if err := s.decorateAPICallLogs(logs); err != nil {
		return nil, err
	}
	for index := range logs {
		// 原始报文只允许通过详情接口按单条读取。
		logs[index].RequestBody = ""
		logs[index].ResponseBody = ""
	}
	page, limit := query.Page, query.Limit
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return &APICallLogPage{Logs: logs, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) decorateAPICallLogs(logs []model.ApiCallLog) error {
	// 历史日志允许读取逻辑删除渠道的名称，但不读取或返回渠道密钥。
	channels, err := s.repo.HistoricalSystemChannelReferences()
	if err != nil {
		return err
	}
	channelNames := make(map[string]string, len(channels))
	for _, channel := range channels {
		channelNames[channel.ID] = channel.Name
	}
	users, err := s.repo.Users()
	if err != nil {
		return err
	}
	userByID := make(map[string]model.User, len(users))
	for _, user := range users {
		userByID[user.ID] = user
	}
	billingOrderIDs := make([]string, 0, len(logs))
	seenBillingOrderIDs := make(map[string]struct{}, len(logs))
	taskIDs := make([]string, 0, len(logs))
	seenTaskIDs := make(map[string]struct{}, len(logs))
	for _, log := range logs {
		if log.Billable && log.BillingOrderID != "" {
			if _, exists := seenBillingOrderIDs[log.BillingOrderID]; !exists {
				seenBillingOrderIDs[log.BillingOrderID] = struct{}{}
				billingOrderIDs = append(billingOrderIDs, log.BillingOrderID)
			}
		}
		if (log.Capability != "image" && log.Capability != "video") || log.TaskID == "" {
			continue
		}
		if _, exists := seenTaskIDs[log.TaskID]; exists {
			continue
		}
		seenTaskIDs[log.TaskID] = struct{}{}
		taskIDs = append(taskIDs, log.TaskID)
	}
	tasks, err := s.repo.APICallLogTasks(taskIDs)
	if err != nil {
		return err
	}
	taskByID := make(map[string]model.Task, len(tasks))
	for _, task := range tasks {
		taskByID[task.ID] = task
	}
	billingOrderByID, err := s.repo.BillingOrdersByIDs(billingOrderIDs)
	if err != nil {
		return err
	}
	for index := range logs {
		if logs[index].StartedAt.IsZero() {
			logs[index].StartedAt = logs[index].CreatedAt
		}
		if logs[index].ChannelID == "" {
			logs[index].ChannelName = "自定义渠道"
		} else if name := channelNames[logs[index].ChannelID]; name != "" {
			logs[index].ChannelName = name
		} else {
			logs[index].ChannelName = "已删除渠道"
		}
		if user, exists := userByID[logs[index].UserID]; exists {
			logs[index].UserDisplayName = user.DisplayName
			logs[index].UserAccount = user.Username
		}
		if logs[index].Billable {
			if order, exists := billingOrderByID[logs[index].BillingOrderID]; exists && order.UserID == logs[index].UserID {
				logs[index].BillingAvailable = true
				logs[index].BillingStatus = order.Status
				if order.Status == model.BillingStatusSettled {
					logs[index].BillingAmount = order.ActualAmountMicrocredits
				} else if order.Status != model.BillingStatusRefunded {
					logs[index].BillingAmount = order.ReservedAmountMicrocredits
				}
			}
		}
		if task, exists := taskByID[logs[index].TaskID]; exists && task.UserID == logs[index].UserID {
			logs[index].TaskStatus = task.Status
			previewURL, previewKind := taskMediaPreview(task.ResultJSON, task.Type)
			if canvasResourceID(previewURL) != "" {
				logs[index].MediaPreviewURL = "/api/admin/api-logs/" + logs[index].ID + "/media"
				logs[index].MediaPreviewKind = previewKind
			} else if strings.HasPrefix(previewURL, "https://") || strings.HasPrefix(previewURL, "http://") {
				logs[index].MediaPreviewURL = previewURL
				logs[index].MediaPreviewKind = previewKind
			}
		}
	}
	return nil
}

// 管理员媒体读取必须同时校验日志、任务和资源归属，不能绕过用户资源边界按资源 ID 任意读取。
func (s *Service) OpenAdminAPICallLogMediaRange(actor *model.User, logID string, rangeHeader string) (*ResourceStream, error) {
	userID, resource, err := s.adminAPICallLogMediaResource(actor, logID)
	if err != nil {
		return nil, err
	}
	return s.openResourceRange(userID, resource, rangeHeader)
}

func (s *Service) PrepareAdminAPICallLogMediaDelivery(actor *model.User, logID string, rangeHeader string) (*ResourceDelivery, error) {
	userID, resource, err := s.adminAPICallLogMediaResource(actor, logID)
	if err != nil {
		return nil, err
	}
	delivery, err := s.prepareResourceDelivery(userID, resource, ResourceDeliveryOptions{})
	if err != nil || delivery.RedirectURL != "" {
		return delivery, err
	}
	stream, err := s.openResourceRange(userID, resource, rangeHeader)
	if err != nil {
		return nil, err
	}
	delivery.Stream = stream
	return delivery, nil
}

func (s *Service) adminAPICallLogMediaResource(actor *model.User, logID string) (string, *model.Resource, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return "", nil, err
	}
	log, err := s.repo.APICallLog(strings.TrimSpace(logID))
	if err != nil {
		return "", nil, err
	}
	if log.TaskID == "" || (log.Capability != "image" && log.Capability != "video") {
		return "", nil, BadAuthRequest("该请求没有可预览媒体")
	}
	task, err := s.repo.Task(log.TaskID)
	if err != nil {
		return "", nil, err
	}
	if task.UserID != log.UserID {
		return "", nil, BadAuthRequest("请求与媒体归属不一致")
	}
	previewURL, _ := taskMediaPreview(task.ResultJSON, task.Type)
	resourceID := canvasResourceID(previewURL)
	if resourceID == "" {
		return "", nil, BadAuthRequest("该请求没有已持久化媒体")
	}
	resource, err := s.repo.ResourceForUser(log.UserID, resourceID)
	if err != nil {
		return "", nil, err
	}
	return log.UserID, resource, nil
}

func (s *Service) AdminAPICallLog(actor *model.User, id string) (*model.ApiCallLog, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	log, err := s.repo.APICallLog(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	logs := []model.ApiCallLog{*log}
	if err := s.decorateAPICallLogs(logs); err != nil {
		return nil, err
	}
	return &logs[0], nil
}

func (s *Service) AdminAPICallLogsCSV(actor *model.User, query APICallLogQuery) ([]byte, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if query.RecordType != "" && query.RecordType != "request" && query.RecordType != "download" && query.RecordType != "all" {
		return nil, BadAuthRequest("请求明细类型无效")
	}
	filter := normalizeAnalyticsFilter(query.AnalyticsQuery)
	ids := uniqueNonEmpty(query.IDs)
	if len(query.IDs) > 0 && len(ids) == 0 {
		return nil, BadAuthRequest("请选择要导出的请求明细")
	}
	if len(ids) > 200 {
		return nil, BadAuthRequest("单次最多导出 200 条已选请求明细")
	}
	logs, err := s.repo.ExportAPICallLogs(repository.APICallLogFilter{AnalyticsFilter: filter, RecordType: query.RecordType, Keyword: query.Keyword, Status: query.Status, IDs: ids}, 10_000)
	if err != nil {
		return nil, err
	}
	if err := s.decorateAPICallLogs(logs); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	buffer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"时间", "用户", "用户账号", "渠道", "模型", "能力", "状态", "轮询次数", "耗时毫秒", "输入Token", "输出Token", "缓存Token", "积分计费(微积分)", "积分计费状态", "上游估算费用(微单位)", "币种", "错误码", "错误"})
	for _, log := range logs {
		startedAt := log.StartedAt
		if startedAt.IsZero() {
			startedAt = log.CreatedAt
		}
		billingAmount, billingStatus := "", ""
		if log.BillingAvailable {
			billingAmount = strconv.FormatInt(log.BillingAmount, 10)
			billingStatus = string(log.BillingStatus)
		}
		upstreamCost := ""
		if log.CostAvailable {
			upstreamCost = strconv.FormatInt(log.EstimatedCostMicros, 10)
		}
		_ = writer.Write([]string{startedAt.UTC().Format(time.RFC3339), log.UserDisplayName, log.UserAccount, log.ChannelName, log.Model, log.Capability, string(log.Status), strconv.Itoa(log.PollCount), strconv.FormatInt(log.DurationMs, 10), strconv.FormatInt(log.InputTokens, 10), strconv.FormatInt(log.OutputTokens, 10), strconv.FormatInt(log.CachedTokens, 10), billingAmount, billingStatus, upstreamCost, log.Currency, log.ErrorCode, log.Error})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Service) AdminAnalyticsCSV(actor *model.User, query AnalyticsQuery) ([]byte, error) {
	overview, err := s.AdminAnalytics(actor, query)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	buffer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"用户", "用户ID", "活跃天数", "登录次数", "首次活跃", "最后活跃", "任务总数", "文本任务", "图片任务", "视频任务", "音频任务", "成功任务", "失败任务", "取消任务", "排队任务", "运行任务", "任务成功率(%)", "平均任务耗时毫秒", "P95任务耗时毫秒", "上游请求", "成功请求", "失败请求", "请求成功率(%)", "P95请求耗时毫秒", "输出媒体数", "生成图片数", "生成视频数", "生成音频数", "生成视频秒数", "输入Token", "输出Token", "缓存Token", "已结算消耗(微积分)", "上游估算费用(微单位)", "币种", "Agent消息", "画布活跃天数", "素材", "资源", "常用模型", "模型明细(JSON)", "渠道明细(JSON)", "每日明细(JSON)"})
	for _, row := range overview.Users {
		tokens := []string{"", "", ""}
		if row.UsageAvailable {
			tokens = []string{strconv.FormatInt(row.InputTokens, 10), strconv.FormatInt(row.OutputTokens, 10), strconv.FormatInt(row.CachedTokens, 10)}
		}
		cost := ""
		if row.CostAvailable {
			cost = strconv.FormatInt(row.EstimatedCostMicros, 10)
		}
		modelsJSON, _ := json.Marshal(row.Models)
		channelsJSON, _ := json.Marshal(row.Channels)
		dailyJSON, _ := json.Marshal(row.Daily)
		_ = writer.Write([]string{row.Name, row.UserID, strconv.Itoa(row.ActiveDays), strconv.Itoa(row.LoginCount), analyticsCSVTime(row.FirstActiveAt), analyticsCSVTime(row.LastActiveAt), strconv.Itoa(row.Tasks), strconv.Itoa(row.TextTasks), strconv.Itoa(row.ImageTasks), strconv.Itoa(row.VideoTasks), strconv.Itoa(row.AudioTasks), strconv.Itoa(row.SucceededTasks), strconv.Itoa(row.FailedTasks), strconv.Itoa(row.CancelledTasks), strconv.Itoa(row.QueuedTasks), strconv.Itoa(row.RunningTasks), strconv.FormatFloat(row.TaskSuccessRate, 'f', 2, 64), strconv.FormatInt(row.AverageTaskDurationMs, 10), strconv.FormatInt(row.P95TaskDurationMs, 10), strconv.Itoa(row.Requests), strconv.Itoa(row.SucceededRequests), strconv.Itoa(row.FailedRequests), strconv.FormatFloat(row.RequestSuccessRate, 'f', 2, 64), strconv.FormatInt(row.P95RequestDurationMs, 10), strconv.Itoa(row.MediaCount), strconv.Itoa(row.GeneratedImages), strconv.Itoa(row.GeneratedVideos), strconv.Itoa(row.GeneratedAudio), strconv.Itoa(row.VideoSeconds), tokens[0], tokens[1], tokens[2], strconv.FormatInt(row.CreditsConsumedMicrocredits, 10), cost, row.Currency, strconv.Itoa(row.AgentMessages), strconv.Itoa(row.CanvasDays), strconv.Itoa(row.Assets), strconv.Itoa(row.Resources), row.CommonModel, string(modelsJSON), string(channelsJSON), string(dailyJSON)})
	}
	writer.Flush()
	return buffer.Bytes(), writer.Error()
}

func (s *Service) AdminModelPricings(actor *model.User) ([]model.ModelPricing, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return s.repo.ModelPricings()
}

func (s *Service) SaveModelPricing(actor *model.User, id string, req ModelPricingRequest) (*model.ModelPricing, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	req.Model = strings.TrimSpace(req.Model)
	req.Capability = normalizeCapability(req.Capability)
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if req.Model == "" || req.Capability == "" {
		return nil, BadAuthRequest("请填写模型并选择能力类型")
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	if len(req.Currency) > 12 || hasNegativePricing(req) {
		return nil, BadAuthRequest("价格配置格式无效")
	}
	pricing := &model.ModelPricing{ID: newID(), CreatedAt: time.Now()}
	if id != "" {
		current, err := s.repo.ModelPricingByID(id)
		if err != nil {
			return nil, err
		}
		pricing = current
	}
	pricing.ChannelID = strings.TrimSpace(req.ChannelID)
	pricing.Model = req.Model
	pricing.Capability = req.Capability
	pricing.Currency = req.Currency
	pricing.InputPerMillionMicros = req.InputPerMillionMicros
	pricing.OutputPerMillionMicros = req.OutputPerMillionMicros
	pricing.CachedPerMillionMicros = req.CachedPerMillionMicros
	pricing.PerRequestMicros = req.PerRequestMicros
	pricing.PerMediaMicros = req.PerMediaMicros
	pricing.PerVideoSecondMicros = req.PerVideoSecondMicros
	pricing.UpdatedAt = time.Now()
	if err := s.repo.Save(pricing); err != nil {
		return nil, err
	}
	return pricing, nil
}

func (s *Service) DeleteModelPricing(actor *model.User, id string) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	return s.repo.DeleteModelPricing(id)
}

func hasNegativePricing(req ModelPricingRequest) bool {
	return req.InputPerMillionMicros < 0 || req.OutputPerMillionMicros < 0 || req.CachedPerMillionMicros < 0 || req.PerRequestMicros < 0 || req.PerMediaMicros < 0 || req.PerVideoSecondMicros < 0
}

func normalizeAnalyticsFilter(query AnalyticsQuery) repository.AnalyticsFilter {
	now := time.Now().UTC()
	to := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -30)
	if parsed, ok := parseAnalyticsTime(query.From); ok {
		from = parsed
	}
	if parsed, ok := parseAnalyticsTime(query.To); ok {
		to = parsed
		if len(strings.TrimSpace(query.To)) == len("2006-01-02") {
			to = to.AddDate(0, 0, 1)
		}
	}
	if !to.After(from) {
		to = from.AddDate(0, 0, 1)
	}
	if to.Sub(from) > 366*24*time.Hour {
		from = to.AddDate(-1, 0, 0)
	}
	return repository.AnalyticsFilter{From: from, To: to, UserID: strings.TrimSpace(query.UserID), Model: strings.TrimSpace(query.Model), ChannelID: strings.TrimSpace(query.ChannelID), Capability: normalizeCapability(query.Capability)}
}

func parseAnalyticsTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func normalizeCapability(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "text", "image", "video", "audio":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func buildAnalyticsOverview(filter repository.AnalyticsFilter, tasks []model.Task, rollingTasks []model.Task, rollingLogs []model.ApiCallLog, logs []model.ApiCallLog, billingOrders []model.BillingOrder, activities []model.UserDailyActivity, users []model.User) *AnalyticsOverview {
	result := &AnalyticsOverview{From: filter.From, To: filter.To, Trend: []AnalyticsTrendPoint{}, Models: []AnalyticsModelRow{}, Channels: []AnalyticsChannelRow{}, Users: []AnalyticsUserRow{}, Failures: []AnalyticsFailureRow{}}
	videoSecondsByTask := generatedVideoSecondsByTask(tasks, logs)
	result.KPI.GenerationTasks = len(tasks)
	result.KPI.UpstreamRequests = len(logs)
	result.KPI.SuccessRate = successRateLogs(logs)
	durations := make([]int64, 0, len(logs))
	var taskDurationTotal int64
	taskDurationCount := 0
	activeUsers := map[string]bool{}
	if !hasCreationDimensionFilter(filter) {
		for _, activity := range activities {
			if activity.Day.Before(filter.From) || !activity.Day.Before(filter.To) || !meaningfulActivity(activity) {
				continue
			}
			activeUsers[activity.UserID] = true
		}
	}
	for _, task := range tasks {
		activeUsers[task.UserID] = true
		capability := capabilityFromTaskType(task.Type)
		incrementCapabilityCount(capability, &result.KPI.TextTasks, &result.KPI.ImageTasks, &result.KPI.VideoTasks, &result.KPI.AudioTasks)
		switch task.Status {
		case model.TaskStatusSucceeded:
			result.KPI.SucceededTasks++
		case model.TaskStatusFailed:
			result.KPI.FailedTasks++
		case model.TaskStatusCancelled:
			result.KPI.CancelledTasks++
		case model.TaskStatusQueued:
			result.KPI.QueuedTasks++
		case model.TaskStatusRunning:
			result.KPI.RunningTasks++
		}
		mediaCount := taskGeneratedMediaCount(task)
		result.KPI.MediaCount += mediaCount
		incrementGeneratedMediaCount(capability, mediaCount, &result.KPI.GeneratedImages, &result.KPI.GeneratedVideos, &result.KPI.GeneratedAudio)
		result.KPI.VideoSeconds += videoSecondsByTask[task.ID]
		if duration := taskDurationMs(task); duration > 0 {
			taskDurationTotal += duration
			taskDurationCount++
		}
	}
	for _, log := range logs {
		activeUsers[log.UserID] = true
		if log.Status == model.ApiCallStatusSucceeded {
			result.KPI.SucceededRequests++
		} else if log.Status == model.ApiCallStatusFailed {
			result.KPI.FailedRequests++
		}
	}
	result.KPI.ActiveUsers = len(activeUsers)
	rollingActivities := activities
	if hasCreationDimensionFilter(filter) {
		rollingActivities = nil
	}
	result.KPI.DAU = rollingActiveUsers(rollingActivities, rollingTasks, rollingLogs, filter.To.AddDate(0, 0, -1), filter.To)
	result.KPI.WAU = rollingActiveUsers(rollingActivities, rollingTasks, rollingLogs, filter.To.AddDate(0, 0, -7), filter.To)
	result.KPI.MAU = rollingActiveUsers(rollingActivities, rollingTasks, rollingLogs, filter.To.AddDate(0, 0, -30), filter.To)
	currency := ""
	for _, log := range logs {
		durations = append(durations, log.DurationMs)
		if log.UsageAvailable {
			result.KPI.UsageAvailable = true
			result.KPI.InputTokens += log.InputTokens
			result.KPI.OutputTokens += log.OutputTokens
			result.KPI.CachedTokens += log.CachedTokens
		}
		if log.CostAvailable {
			result.KPI.CostAvailable = true
			result.KPI.EstimatedCostMicros += log.EstimatedCostMicros
			currency = mergeCurrency(currency, log.Currency)
		}
	}
	for _, order := range billingOrders {
		result.KPI.CreditsConsumedMicrocredits += order.ActualAmountMicrocredits
	}
	result.KPI.TaskSuccessRate = ratio(result.KPI.SucceededTasks, result.KPI.SucceededTasks+result.KPI.FailedTasks)
	if taskDurationCount > 0 {
		result.KPI.AverageTaskDurationMs = taskDurationTotal / int64(taskDurationCount)
	}
	result.KPI.Currency = currency
	result.KPI.P95DurationMs = percentile(durations, 0.95)
	result.Trend = buildAnalyticsTrend(filter, tasks, logs, activities, videoSecondsByTask)
	result.Models = buildAnalyticsModels(tasks, logs, billingOrders, videoSecondsByTask)
	result.Channels = buildAnalyticsChannels(tasks, logs, billingOrders, videoSecondsByTask)
	result.Users = buildAnalyticsUsers(filter, tasks, logs, billingOrders, activities, users, videoSecondsByTask)
	result.Failures = buildAnalyticsFailures(logs)
	return result
}

func buildAnalyticsTrend(filter repository.AnalyticsFilter, tasks []model.Task, logs []model.ApiCallLog, activities []model.UserDailyActivity, videoSecondsByTask map[string]int) []AnalyticsTrendPoint {
	points := map[string]*AnalyticsTrendPoint{}
	for day := time.Date(filter.From.Year(), filter.From.Month(), filter.From.Day(), 0, 0, 0, 0, time.UTC); day.Before(filter.To); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		points[key] = &AnalyticsTrendPoint{Day: key}
	}
	requestTotals := map[string]int{}
	requestSuccess := map[string]int{}
	taskTotals := map[string]int{}
	activeByDay := map[string]map[string]bool{}
	for _, task := range tasks {
		key := task.CreatedAt.UTC().Format("2006-01-02")
		if point := points[key]; point != nil {
			point.Tasks++
			incrementCapabilityCount(capabilityFromTaskType(task.Type), &point.TextTasks, &point.ImageTasks, &point.VideoTasks, &point.AudioTasks)
			if task.Status == model.TaskStatusSucceeded {
				point.SucceededTasks++
				taskTotals[key]++
			} else if task.Status == model.TaskStatusFailed {
				point.FailedTasks++
				taskTotals[key]++
			}
			point.MediaCount += taskGeneratedMediaCount(task)
			point.VideoSeconds += videoSecondsByTask[task.ID]
			if activeByDay[key] == nil {
				activeByDay[key] = map[string]bool{}
			}
			activeByDay[key][task.UserID] = true
		}
	}
	for _, log := range logs {
		key := log.CreatedAt.UTC().Format("2006-01-02")
		if point := points[key]; point != nil {
			point.Requests++
			requestTotals[key]++
			if log.Status == model.ApiCallStatusSucceeded {
				requestSuccess[key]++
			}
			if activeByDay[key] == nil {
				activeByDay[key] = map[string]bool{}
			}
			activeByDay[key][log.UserID] = true
		}
	}
	if !hasCreationDimensionFilter(filter) {
		for _, activity := range activities {
			key := activity.Day.UTC().Format("2006-01-02")
			if points[key] == nil || !meaningfulActivity(activity) {
				continue
			}
			if activeByDay[key] == nil {
				activeByDay[key] = map[string]bool{}
			}
			activeByDay[key][activity.UserID] = true
		}
	}
	keys := make([]string, 0, len(points))
	for key := range points {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]AnalyticsTrendPoint, 0, len(keys))
	for _, key := range keys {
		point := points[key]
		point.ActiveUsers = len(activeByDay[key])
		point.TaskSuccessRate = ratio(point.SucceededTasks, taskTotals[key])
		point.RequestSuccessRate = ratio(requestSuccess[key], requestTotals[key])
		result = append(result, *point)
	}
	return result
}

func buildAnalyticsModels(tasks []model.Task, logs []model.ApiCallLog, billingOrders []model.BillingOrder, videoSecondsByTask map[string]int) []AnalyticsModelRow {
	type accumulator struct {
		row            AnalyticsModelRow
		users          map[string]bool
		taskSuccess    int
		taskTotal      int
		requestSuccess int
		durations      []int64
	}
	items := map[string]*accumulator{}
	taskByID := make(map[string]model.Task, len(tasks))
	get := func(modelName string, capability string) *accumulator {
		if modelName == "" {
			modelName = "未识别"
		}
		key := modelName + "\x00" + capability
		if items[key] == nil {
			items[key] = &accumulator{row: AnalyticsModelRow{Model: modelName, Capability: capability}, users: map[string]bool{}}
		}
		return items[key]
	}
	for _, task := range tasks {
		taskByID[task.ID] = task
		capability := capabilityFromTaskType(task.Type)
		item := get(task.Model, capability)
		item.row.Tasks++
		item.users[task.UserID] = true
		switch task.Status {
		case model.TaskStatusSucceeded:
			item.row.SucceededTasks++
		case model.TaskStatusFailed:
			item.row.FailedTasks++
		case model.TaskStatusCancelled:
			item.row.CancelledTasks++
		case model.TaskStatusQueued:
			item.row.QueuedTasks++
		case model.TaskStatusRunning:
			item.row.RunningTasks++
		}
		if task.Status == model.TaskStatusSucceeded || task.Status == model.TaskStatusFailed {
			item.taskTotal++
		}
		if task.Status == model.TaskStatusSucceeded {
			item.taskSuccess++
		}
		mediaCount := taskGeneratedMediaCount(task)
		item.row.MediaCount += mediaCount
		incrementGeneratedMediaCount(capability, mediaCount, &item.row.GeneratedImages, &item.row.GeneratedVideos, &item.row.GeneratedAudio)
		item.row.VideoSeconds += videoSecondsByTask[task.ID]
	}
	for _, log := range logs {
		modelName, capability := log.Model, log.Capability
		if task, exists := taskByID[log.TaskID]; exists && task.UserID == log.UserID {
			modelName = firstNonEmpty(task.Model, modelName)
			capability = firstNonEmpty(capabilityFromTaskType(task.Type), capability)
		}
		item := get(modelName, capability)
		item.row.Requests++
		item.users[log.UserID] = true
		item.durations = append(item.durations, log.DurationMs)
		if log.Status == model.ApiCallStatusSucceeded {
			item.requestSuccess++
			item.row.SucceededRequests++
		} else if log.Status == model.ApiCallStatusFailed {
			item.row.FailedRequests++
		}
		if log.UsageAvailable {
			item.row.UsageAvailable = true
			item.row.InputTokens += log.InputTokens
			item.row.OutputTokens += log.OutputTokens
			item.row.CachedTokens += log.CachedTokens
		}
		if log.CostAvailable {
			item.row.CostAvailable = true
			item.row.EstimatedCostMicros += log.EstimatedCostMicros
			item.row.Currency = mergeCurrency(item.row.Currency, log.Currency)
		}
	}
	for _, order := range billingOrders {
		modelName, capability := order.Model, order.Capability
		if task, exists := taskByID[order.TaskID]; exists && task.UserID == order.UserID {
			modelName = firstNonEmpty(task.Model, modelName)
			capability = firstNonEmpty(capabilityFromTaskType(task.Type), capability)
		}
		item := get(modelName, capability)
		item.row.CreditsConsumedMicrocredits += order.ActualAmountMicrocredits
		item.users[order.UserID] = true
	}
	result := make([]AnalyticsModelRow, 0, len(items))
	for _, item := range items {
		item.row.UniqueUsers = len(item.users)
		item.row.TaskSuccessRate = ratio(item.taskSuccess, item.taskTotal)
		item.row.RequestSuccessRate = ratio(item.requestSuccess, item.row.Requests)
		item.row.P50DurationMs = percentile(item.durations, 0.5)
		item.row.P95DurationMs = percentile(item.durations, 0.95)
		result = append(result, item.row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Tasks == result[j].Tasks {
			return result[i].Requests > result[j].Requests
		}
		return result[i].Tasks > result[j].Tasks
	})
	return result
}

func buildAnalyticsChannels(tasks []model.Task, logs []model.ApiCallLog, billingOrders []model.BillingOrder, videoSecondsByTask map[string]int) []AnalyticsChannelRow {
	type accumulator struct {
		row       AnalyticsChannelRow
		users     map[string]bool
		models    map[string]bool
		durations []int64
	}
	items := map[string]*accumulator{}
	get := func(channelID string) *accumulator {
		key := channelID
		if items[key] == nil {
			name := channelID
			if name == "" {
				name = "本地、自定义或未关联"
			}
			items[key] = &accumulator{row: AnalyticsChannelRow{ChannelID: channelID, Name: name}, users: map[string]bool{}, models: map[string]bool{}}
		}
		return items[key]
	}

	taskChannel := analyticsTaskChannels(tasks, logs, billingOrders)

	for _, task := range tasks {
		item := get(taskChannel[task.ID])
		capability := capabilityFromTaskType(task.Type)
		mediaCount := taskGeneratedMediaCount(task)
		item.row.Tasks++
		item.row.MediaCount += mediaCount
		incrementGeneratedMediaCount(capability, mediaCount, &item.row.GeneratedImages, &item.row.GeneratedVideos, &item.row.GeneratedAudio)
		item.row.VideoSeconds += videoSecondsByTask[task.ID]
		item.users[task.UserID] = true
		if task.Model != "" {
			item.models[task.Model] = true
		}
	}
	for _, log := range logs {
		item := get(log.ChannelID)
		item.row.Requests++
		item.users[log.UserID] = true
		if log.Model != "" {
			item.models[log.Model] = true
		}
		item.durations = append(item.durations, log.DurationMs)
		if log.Status == model.ApiCallStatusSucceeded {
			item.row.SucceededRequests++
		} else if log.Status == model.ApiCallStatusFailed {
			item.row.FailedRequests++
		}
		if log.UsageAvailable {
			item.row.UsageAvailable = true
			item.row.InputTokens += log.InputTokens
			item.row.OutputTokens += log.OutputTokens
			item.row.CachedTokens += log.CachedTokens
		}
		if log.CostAvailable {
			item.row.CostAvailable = true
			item.row.EstimatedCostMicros += log.EstimatedCostMicros
			item.row.Currency = mergeCurrency(item.row.Currency, log.Currency)
		}
	}
	for _, order := range billingOrders {
		channelID := order.ChannelID
		if channelID == "" {
			channelID = taskChannel[order.TaskID]
		}
		item := get(channelID)
		item.row.CreditsConsumedMicrocredits += order.ActualAmountMicrocredits
		item.users[order.UserID] = true
		if order.Model != "" {
			item.models[order.Model] = true
		}
	}

	result := make([]AnalyticsChannelRow, 0, len(items))
	for _, item := range items {
		item.row.UniqueUsers = len(item.users)
		item.row.UniqueModels = len(item.models)
		item.row.RequestSuccessRate = ratio(item.row.SucceededRequests, item.row.SucceededRequests+item.row.FailedRequests)
		item.row.P50DurationMs = percentile(item.durations, 0.5)
		item.row.P95DurationMs = percentile(item.durations, 0.95)
		result = append(result, item.row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Requests == result[j].Requests {
			return result[i].Tasks > result[j].Tasks
		}
		return result[i].Requests > result[j].Requests
	})
	return result
}

func buildAnalyticsUsers(filter repository.AnalyticsFilter, tasks []model.Task, logs []model.ApiCallLog, billingOrders []model.BillingOrder, activities []model.UserDailyActivity, users []model.User, videoSecondsByTask map[string]int) []AnalyticsUserRow {
	type channelAccumulator struct {
		row    AnalyticsUserChannelRow
		models map[string]bool
	}
	type accumulator struct {
		row               AnalyticsUserRow
		activeDays        map[string]bool
		taskDurationTotal int64
		taskDurationCount int
		taskFinished      int
		requestSuccess    int
		taskDurations     []int64
		requestDurations  []int64
		models            map[string]*AnalyticsUserModelRow
		channels          map[string]*channelAccumulator
		daily             map[string]*AnalyticsUserDayRow
	}
	names := map[string]string{}
	for _, user := range users {
		names[user.ID] = firstNonEmpty(user.DisplayName, user.Username)
	}
	rows := map[string]*accumulator{}
	taskByID := make(map[string]model.Task, len(tasks))
	get := func(userID string) *accumulator {
		if rows[userID] == nil {
			rows[userID] = &accumulator{
				row:        AnalyticsUserRow{UserID: userID, Name: firstNonEmpty(names[userID], userID), Models: []AnalyticsUserModelRow{}, Channels: []AnalyticsUserChannelRow{}, Daily: []AnalyticsUserDayRow{}},
				activeDays: map[string]bool{},
				models:     map[string]*AnalyticsUserModelRow{},
				channels:   map[string]*channelAccumulator{},
				daily:      map[string]*AnalyticsUserDayRow{},
			}
		}
		return rows[userID]
	}
	getDay := func(item *accumulator, day string) *AnalyticsUserDayRow {
		if item.daily[day] == nil {
			item.daily[day] = &AnalyticsUserDayRow{Day: day}
		}
		return item.daily[day]
	}
	getModel := func(item *accumulator, modelName string, capability string) *AnalyticsUserModelRow {
		modelName = firstNonEmpty(modelName, "未识别")
		key := modelName + "\x00" + capability
		if item.models[key] == nil {
			item.models[key] = &AnalyticsUserModelRow{Model: modelName, Capability: capability}
		}
		return item.models[key]
	}
	getChannel := func(item *accumulator, channelID string) *channelAccumulator {
		if item.channels[channelID] == nil {
			name := channelID
			if name == "" {
				name = "本地、自定义或未关联"
			}
			item.channels[channelID] = &channelAccumulator{row: AnalyticsUserChannelRow{ChannelID: channelID, Name: name}, models: map[string]bool{}}
		}
		return item.channels[channelID]
	}
	taskChannel := analyticsTaskChannels(tasks, logs, billingOrders)
	if !hasCreationDimensionFilter(filter) {
		for _, user := range users {
			if filter.UserID == "" || filter.UserID == user.ID {
				get(user.ID)
			}
		}
	}
	if !hasCreationDimensionFilter(filter) {
		for _, activity := range activities {
			if activity.Day.Before(filter.From) || !activity.Day.Before(filter.To) {
				continue
			}
			item := get(activity.UserID)
			dayKey := activity.Day.UTC().Format("2006-01-02")
			day := getDay(item, dayKey)
			item.row.LoginCount += activity.LoginCount
			item.row.AgentMessages += activity.AgentMessageCount
			if activity.CanvasActive {
				item.row.CanvasDays++
			}
			item.row.Assets += activity.AssetCount
			item.row.Resources += activity.ResourceCount
			day.LoginCount += activity.LoginCount
			day.AgentMessages += activity.AgentMessageCount
			day.CanvasActive = day.CanvasActive || activity.CanvasActive
			day.Assets += activity.AssetCount
			day.Resources += activity.ResourceCount
			if meaningfulActivity(activity) {
				item.activeDays[dayKey] = true
			}
			if activity.FirstActiveAt != nil {
				updateAnalyticsUserActivityBounds(&item.row, *activity.FirstActiveAt)
			}
			if activity.LastActiveAt != nil {
				updateAnalyticsUserActivityBounds(&item.row, *activity.LastActiveAt)
			}
			if activity.LoginCount > 0 {
				if activity.FirstActiveAt == nil && !activity.CreatedAt.IsZero() {
					updateAnalyticsUserActivityBounds(&item.row, activity.CreatedAt)
				}
				if activity.LastActiveAt == nil && !activity.UpdatedAt.IsZero() {
					updateAnalyticsUserActivityBounds(&item.row, activity.UpdatedAt)
				}
			}
		}
	}
	for _, task := range tasks {
		taskByID[task.ID] = task
		item := get(task.UserID)
		dayKey := task.CreatedAt.UTC().Format("2006-01-02")
		day := getDay(item, dayKey)
		item.activeDays[dayKey] = true
		updateAnalyticsUserActivityBounds(&item.row, task.CreatedAt)
		if task.CompletedAt != nil {
			updateAnalyticsUserActivityBounds(&item.row, *task.CompletedAt)
		}
		capability := capabilityFromTaskType(task.Type)
		modelRow := getModel(item, task.Model, capability)
		channel := getChannel(item, taskChannel[task.ID])
		item.row.Tasks++
		day.Tasks++
		modelRow.Tasks++
		incrementCapabilityCount(capability, &item.row.TextTasks, &item.row.ImageTasks, &item.row.VideoTasks, &item.row.AudioTasks)
		incrementCapabilityCount(capability, &day.TextTasks, &day.ImageTasks, &day.VideoTasks, &day.AudioTasks)
		channel.row.Tasks++
		if task.Model != "" {
			channel.models[task.Model] = true
		}
		switch task.Status {
		case model.TaskStatusSucceeded:
			item.row.SucceededTasks++
			day.SucceededTasks++
			modelRow.SucceededTasks++
			item.taskFinished++
		case model.TaskStatusFailed:
			item.row.FailedTasks++
			day.FailedTasks++
			modelRow.FailedTasks++
			item.taskFinished++
		case model.TaskStatusCancelled:
			item.row.CancelledTasks++
			day.CancelledTasks++
		case model.TaskStatusQueued:
			item.row.QueuedTasks++
			day.QueuedTasks++
		case model.TaskStatusRunning:
			item.row.RunningTasks++
			day.RunningTasks++
		}
		mediaCount := taskGeneratedMediaCount(task)
		videoSeconds := videoSecondsByTask[task.ID]
		item.row.MediaCount += mediaCount
		day.MediaCount += mediaCount
		modelRow.MediaCount += mediaCount
		channel.row.MediaCount += mediaCount
		incrementGeneratedMediaCount(capability, mediaCount, &item.row.GeneratedImages, &item.row.GeneratedVideos, &item.row.GeneratedAudio)
		incrementGeneratedMediaCount(capability, mediaCount, &day.GeneratedImages, &day.GeneratedVideos, &day.GeneratedAudio)
		incrementGeneratedMediaCount(capability, mediaCount, &channel.row.GeneratedImages, &channel.row.GeneratedVideos, &channel.row.GeneratedAudio)
		item.row.VideoSeconds += videoSeconds
		day.VideoSeconds += videoSeconds
		modelRow.VideoSeconds += videoSeconds
		channel.row.VideoSeconds += videoSeconds
		if duration := taskDurationMs(task); duration > 0 {
			item.taskDurationTotal += duration
			item.taskDurationCount++
			item.taskDurations = append(item.taskDurations, duration)
		}
	}
	for _, log := range logs {
		item := get(log.UserID)
		dayKey := log.CreatedAt.UTC().Format("2006-01-02")
		day := getDay(item, dayKey)
		item.activeDays[dayKey] = true
		updateAnalyticsUserActivityBounds(&item.row, log.CreatedAt)
		modelName, capability := log.Model, log.Capability
		if task, exists := taskByID[log.TaskID]; exists && task.UserID == log.UserID {
			modelName = firstNonEmpty(task.Model, modelName)
			capability = firstNonEmpty(capabilityFromTaskType(task.Type), capability)
		}
		modelRow := getModel(item, modelName, capability)
		channel := getChannel(item, log.ChannelID)
		item.row.Requests++
		day.Requests++
		modelRow.Requests++
		channel.row.Requests++
		if modelName != "" {
			channel.models[modelName] = true
		}
		if log.Status == model.ApiCallStatusSucceeded {
			item.requestSuccess++
			item.row.SucceededRequests++
			day.SucceededRequests++
			modelRow.SucceededRequests++
			channel.row.SucceededRequests++
		} else if log.Status == model.ApiCallStatusFailed {
			item.row.FailedRequests++
			day.FailedRequests++
			modelRow.FailedRequests++
			channel.row.FailedRequests++
		}
		item.requestDurations = append(item.requestDurations, log.DurationMs)
		if log.UsageAvailable {
			item.row.UsageAvailable = true
			item.row.InputTokens += log.InputTokens
			item.row.OutputTokens += log.OutputTokens
			item.row.CachedTokens += log.CachedTokens
			day.InputTokens += log.InputTokens
			day.OutputTokens += log.OutputTokens
			day.CachedTokens += log.CachedTokens
			modelRow.UsageAvailable = true
			modelRow.InputTokens += log.InputTokens
			modelRow.OutputTokens += log.OutputTokens
			modelRow.CachedTokens += log.CachedTokens
			channel.row.UsageAvailable = true
			channel.row.InputTokens += log.InputTokens
			channel.row.OutputTokens += log.OutputTokens
			channel.row.CachedTokens += log.CachedTokens
		}
		if log.CostAvailable {
			item.row.CostAvailable = true
			item.row.EstimatedCostMicros += log.EstimatedCostMicros
			item.row.Currency = mergeCurrency(item.row.Currency, log.Currency)
			day.CostAvailable = true
			day.EstimatedCostMicros += log.EstimatedCostMicros
			day.Currency = mergeCurrency(day.Currency, log.Currency)
			modelRow.CostAvailable = true
			modelRow.EstimatedCostMicros += log.EstimatedCostMicros
			modelRow.Currency = mergeCurrency(modelRow.Currency, log.Currency)
			channel.row.CostAvailable = true
			channel.row.EstimatedCostMicros += log.EstimatedCostMicros
			channel.row.Currency = mergeCurrency(channel.row.Currency, log.Currency)
		}
	}
	for _, order := range billingOrders {
		item := get(order.UserID)
		item.row.CreditsConsumedMicrocredits += order.ActualAmountMicrocredits
		settledAt := order.CreatedAt
		if order.SettledAt != nil {
			settledAt = *order.SettledAt
		}
		if !settledAt.Before(filter.From) && settledAt.Before(filter.To) {
			getDay(item, settledAt.UTC().Format("2006-01-02")).CreditsConsumedMicrocredits += order.ActualAmountMicrocredits
		}
		modelName, capability := order.Model, order.Capability
		if task, exists := taskByID[order.TaskID]; exists && task.UserID == order.UserID {
			modelName = firstNonEmpty(task.Model, modelName)
			capability = firstNonEmpty(capabilityFromTaskType(task.Type), capability)
		}
		getModel(item, modelName, capability).CreditsConsumedMicrocredits += order.ActualAmountMicrocredits
		channelID := order.ChannelID
		if channelID == "" {
			channelID = taskChannel[order.TaskID]
		}
		channel := getChannel(item, channelID)
		channel.row.CreditsConsumedMicrocredits += order.ActualAmountMicrocredits
		if modelName != "" {
			channel.models[modelName] = true
		}
	}
	result := make([]AnalyticsUserRow, 0, len(rows))
	for _, item := range rows {
		item.row.ActiveDays = len(item.activeDays)
		item.row.TaskSuccessRate = ratio(item.row.SucceededTasks, item.taskFinished)
		item.row.RequestSuccessRate = ratio(item.requestSuccess, item.row.Requests)
		if item.taskDurationCount > 0 {
			item.row.AverageTaskDurationMs = item.taskDurationTotal / int64(item.taskDurationCount)
		}
		item.row.P95TaskDurationMs = percentile(item.taskDurations, 0.95)
		item.row.P95RequestDurationMs = percentile(item.requestDurations, 0.95)
		for _, modelRow := range item.models {
			modelRow.TaskSuccessRate = ratio(modelRow.SucceededTasks, modelRow.SucceededTasks+modelRow.FailedTasks)
			modelRow.RequestSuccessRate = ratio(modelRow.SucceededRequests, modelRow.SucceededRequests+modelRow.FailedRequests)
			item.row.Models = append(item.row.Models, *modelRow)
		}
		sort.Slice(item.row.Models, func(i, j int) bool {
			if item.row.Models[i].Tasks == item.row.Models[j].Tasks {
				return item.row.Models[i].Requests > item.row.Models[j].Requests
			}
			return item.row.Models[i].Tasks > item.row.Models[j].Tasks
		})
		if len(item.row.Models) > 0 && item.row.Models[0].Tasks > 0 {
			item.row.CommonModel = item.row.Models[0].Model
		}
		for _, channel := range item.channels {
			channel.row.UniqueModels = len(channel.models)
			channel.row.RequestSuccessRate = ratio(channel.row.SucceededRequests, channel.row.SucceededRequests+channel.row.FailedRequests)
			item.row.Channels = append(item.row.Channels, channel.row)
		}
		sort.Slice(item.row.Channels, func(i, j int) bool {
			if item.row.Channels[i].Requests == item.row.Channels[j].Requests {
				return item.row.Channels[i].Tasks > item.row.Channels[j].Tasks
			}
			return item.row.Channels[i].Requests > item.row.Channels[j].Requests
		})
		for _, day := range item.daily {
			item.row.Daily = append(item.row.Daily, *day)
		}
		sort.Slice(item.row.Daily, func(i, j int) bool { return item.row.Daily[i].Day < item.row.Daily[j].Day })
		result = append(result, item.row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Tasks == result[j].Tasks {
			return result[i].ActiveDays > result[j].ActiveDays
		}
		return result[i].Tasks > result[j].Tasks
	})
	return result
}

func buildAnalyticsFailures(logs []model.ApiCallLog) []AnalyticsFailureRow {
	items := map[string]*AnalyticsFailureRow{}
	for _, log := range logs {
		if log.Status != model.ApiCallStatusFailed {
			continue
		}
		typeName := classifyAPICallError(log)
		modelName := firstNonEmpty(log.Model, "未识别")
		key := typeName + "\x00" + modelName
		if items[key] == nil {
			items[key] = &AnalyticsFailureRow{Type: typeName, Model: modelName}
		}
		item := items[key]
		item.Count++
		if log.CreatedAt.After(item.LastSeenAt) {
			item.LastSeenAt = log.CreatedAt
			item.LastError = truncateRunes(log.Error, 180)
		}
	}
	result := make([]AnalyticsFailureRow, 0, len(items))
	for _, item := range items {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Count > result[j].Count })
	return result
}

func classifyAPICallError(log model.ApiCallLog) string {
	value := strings.ToLower(log.Error)
	switch {
	case log.ErrorCode == contentModerationErrorCode:
		return "内容审核"
	case strings.Contains(value, "timeout"), strings.Contains(value, "超时"), log.StatusCode == 408, log.StatusCode == 504, log.StatusCode == 524:
		return "超时"
	case log.StatusCode == 401 || log.StatusCode == 403 || strings.Contains(value, "unauthorized"):
		return "鉴权失败"
	case log.StatusCode == 429 || strings.Contains(value, "rate limit"):
		return "限流"
	case log.StatusCode >= 400 && log.StatusCode < 500:
		return "请求参数"
	case log.StatusCode >= 500:
		return "上游服务"
	case value != "":
		return "网络或客户端"
	default:
		return "未知错误"
	}
}

func meaningfulActivity(activity model.UserDailyActivity) bool {
	return activity.LoginCount > 0 || activity.TaskCount > 0 || activity.AgentMessageCount > 0 || activity.CanvasActive || activity.AssetCount > 0 || activity.ResourceCount > 0
}

func rollingActiveUsers(activities []model.UserDailyActivity, tasks []model.Task, logs []model.ApiCallLog, from time.Time, to time.Time) int {
	users := map[string]bool{}
	for _, activity := range activities {
		if !activity.Day.Before(from) && activity.Day.Before(to) && meaningfulActivity(activity) {
			users[activity.UserID] = true
		}
	}
	for _, task := range tasks {
		if !task.CreatedAt.Before(from) && task.CreatedAt.Before(to) {
			users[task.UserID] = true
		}
	}
	for _, log := range logs {
		if !log.CreatedAt.Before(from) && log.CreatedAt.Before(to) {
			users[log.UserID] = true
		}
	}
	return len(users)
}

func successRateLogs(logs []model.ApiCallLog) float64 {
	succeeded, failed := 0, 0
	for _, log := range logs {
		if log.Status == model.ApiCallStatusSucceeded {
			succeeded++
		} else if log.Status == model.ApiCallStatusFailed {
			failed++
		}
	}
	return ratio(succeeded, succeeded+failed)
}

func ratio(value int, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(value) * 100 / float64(total)
}

func percentile(values []int64, quantile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	items := append([]int64(nil), values...)
	sort.Slice(items, func(i, j int) bool { return items[i] < items[j] })
	index := int(float64(len(items)-1)*quantile + 0.5)
	return items[index]
}

func mergeCurrency(current string, next string) string {
	if next == "" {
		return current
	}
	if current == "" || current == next {
		return next
	}
	return "MIXED"
}

func capabilityFromTaskType(taskType string) string {
	value := strings.ToLower(taskType)
	for _, capability := range []string{"video", "image", "audio", "text"} {
		if strings.Contains(value, capability) {
			return capability
		}
	}
	if strings.Contains(value, "storyboard") || strings.Contains(value, "agent") {
		return "text"
	}
	return ""
}

func incrementCapabilityCount(capability string, textTasks *int, imageTasks *int, videoTasks *int, audioTasks *int) {
	switch capability {
	case "text":
		*textTasks++
	case "image":
		*imageTasks++
	case "video":
		*videoTasks++
	case "audio":
		*audioTasks++
	}
}

func incrementGeneratedMediaCount(capability string, count int, images *int, videos *int, audio *int) {
	if count <= 0 {
		return
	}
	switch capability {
	case "image":
		*images += count
	case "video":
		*videos += count
	case "audio":
		*audio += count
	}
}

func updateAnalyticsUserActivityBounds(row *AnalyticsUserRow, at time.Time) {
	if at.IsZero() {
		return
	}
	if row.FirstActiveAt == nil || at.Before(*row.FirstActiveAt) {
		value := at
		row.FirstActiveAt = &value
	}
	if row.LastActiveAt == nil || at.After(*row.LastActiveAt) {
		value := at
		row.LastActiveAt = &value
	}
}

func analyticsCSVTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func taskGeneratedMediaCount(task model.Task) int {
	if task.Status != model.TaskStatusSucceeded {
		return 0
	}
	switch capabilityFromTaskType(task.Type) {
	case "image":
		if count := taskImageOutputCount(task.ResultJSON); count > 0 {
			return count
		}
		if count := taskJSONPositiveInt(task.InputJSON, "count"); count > 0 {
			return count
		}
		return 1
	case "video", "audio":
		return 1
	default:
		return 0
	}
}

func taskGeneratedVideoSeconds(task model.Task) int {
	if task.Status != model.TaskStatusSucceeded || capabilityFromTaskType(task.Type) != "video" {
		return 0
	}
	for _, key := range []string{"videoSeconds", "durationSeconds", "duration", "seconds"} {
		if seconds := taskJSONPositiveInt(task.InputJSON, key); seconds > 0 {
			return seconds
		}
	}
	return 0
}

func generatedVideoSecondsByTask(tasks []model.Task, logs []model.ApiCallLog) map[string]int {
	result := make(map[string]int, len(tasks))
	taskSnapshot := make(map[string]bool, len(tasks))
	eligible := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if task.Status != model.TaskStatusSucceeded || capabilityFromTaskType(task.Type) != "video" {
			continue
		}
		eligible[task.ID] = true
		if seconds := taskGeneratedVideoSeconds(task); seconds > 0 {
			result[task.ID] = seconds
			taskSnapshot[task.ID] = true
		}
	}
	for _, log := range logs {
		if !eligible[log.TaskID] || taskSnapshot[log.TaskID] || log.Capability != "video" || log.VideoSeconds <= result[log.TaskID] {
			continue
		}
		result[log.TaskID] = log.VideoSeconds
	}
	return result
}

func taskDurationMs(task model.Task) int64 {
	if task.StartedAt == nil || task.CompletedAt == nil || !task.CompletedAt.After(*task.StartedAt) {
		return 0
	}
	return task.CompletedAt.Sub(*task.StartedAt).Milliseconds()
}

func taskImageOutputCount(raw string) int {
	var payload any
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return 0
	}
	return imageOutputCount(payload)
}

func imageOutputCount(value any) int {
	switch item := value.(type) {
	case []any:
		for _, child := range item {
			if count := imageOutputCount(child); count > 0 {
				return count
			}
		}
	case map[string]any:
		for _, key := range []string{"images", "outputs"} {
			if children, ok := item[key].([]any); ok && len(children) > 0 {
				return len(children)
			}
		}
		for _, key := range []string{"image", "data"} {
			if child, exists := item[key]; exists {
				if children, ok := child.([]any); ok && len(children) > 0 {
					return len(children)
				}
				if count := imageOutputCount(child); count > 0 {
					return count
				}
			}
		}
	}
	return 0
}

func taskJSONPositiveInt(raw string, key string) int {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var payload any
	if decoder.Decode(&payload) != nil {
		return 0
	}
	return nestedPositiveInt(payload, key)
}

func nestedPositiveInt(value any, key string) int {
	switch item := value.(type) {
	case map[string]any:
		if raw, exists := item[key]; exists {
			if parsed := positiveIntValue(raw); parsed > 0 {
				return parsed
			}
		}
		for _, child := range item {
			if parsed := nestedPositiveInt(child, key); parsed > 0 {
				return parsed
			}
		}
	case []any:
		for _, child := range item {
			if parsed := nestedPositiveInt(child, key); parsed > 0 {
				return parsed
			}
		}
	}
	return 0
}

func positiveIntValue(value any) int {
	var parsed int64
	switch item := value.(type) {
	case json.Number:
		parsed, _ = strconv.ParseInt(item.String(), 10, 32)
	case string:
		parsed, _ = strconv.ParseInt(strings.TrimSpace(item), 10, 32)
	case float64:
		parsed = int64(item)
	case int:
		parsed = int64(item)
	case int64:
		parsed = item
	}
	if parsed <= 0 || parsed > int64(^uint(0)>>1) {
		return 0
	}
	return int(parsed)
}

func hasCreationDimensionFilter(filter repository.AnalyticsFilter) bool {
	return filter.Model != "" || filter.ChannelID != "" || filter.Capability != ""
}

func analyticsTaskChannels(tasks []model.Task, logs []model.ApiCallLog, billingOrders []model.BillingOrder) map[string]string {
	type candidate struct {
		channelID string
		rank      int
		at        time.Time
	}
	owners := make(map[string]string, len(tasks))
	selected := make(map[string]candidate, len(tasks))
	for _, task := range tasks {
		owners[task.ID] = task.UserID
	}
	consider := func(taskID string, userID string, channelID string, rank int, at time.Time) {
		owner, exists := owners[taskID]
		if !exists || channelID == "" || (userID != "" && userID != owner) {
			return
		}
		current, exists := selected[taskID]
		if exists && (current.rank > rank || (current.rank == rank && (current.at.After(at) || (current.at.Equal(at) && current.channelID >= channelID)))) {
			return
		}
		selected[taskID] = candidate{channelID: channelID, rank: rank, at: at}
	}
	for _, log := range logs {
		rank := 1
		if log.Status == model.ApiCallStatusSucceeded {
			rank = 2
		}
		consider(log.TaskID, log.UserID, log.ChannelID, rank, log.CreatedAt)
	}
	for _, order := range billingOrders {
		at := order.CreatedAt
		if order.SettledAt != nil {
			at = *order.SettledAt
		}
		consider(order.TaskID, order.UserID, order.ChannelID, 3, at)
	}
	result := make(map[string]string, len(tasks))
	for taskID, item := range selected {
		result[taskID] = item.channelID
	}
	return result
}

func filterAnalyticsFacts(filter repository.AnalyticsFilter, tasks []model.Task, taskReferences []model.Task, logs []model.ApiCallLog, billingOrders []model.BillingOrder) ([]model.Task, []model.ApiCallLog, []model.BillingOrder, error) {
	taskByID := make(map[string]model.Task, len(taskReferences))
	for _, task := range taskReferences {
		taskByID[task.ID] = task
	}
	taskChannels := analyticsTaskChannels(taskReferences, logs, billingOrders)
	matches := func(modelName string, capability string, channelID string) bool {
		return (filter.Model == "" || modelName == filter.Model) &&
			(filter.Capability == "" || capability == filter.Capability) &&
			(filter.ChannelID == "" || channelID == filter.ChannelID)
	}

	filteredTasks := make([]model.Task, 0, len(tasks))
	for _, task := range tasks {
		if matches(task.Model, capabilityFromTaskType(task.Type), taskChannels[task.ID]) {
			filteredTasks = append(filteredTasks, task)
		}
	}
	filteredLogs := make([]model.ApiCallLog, 0, len(logs))
	for _, log := range logs {
		modelName, capability := log.Model, normalizeCapability(log.Capability)
		if task, exists := taskByID[log.TaskID]; exists && task.UserID == log.UserID {
			modelName = firstNonEmpty(task.Model, modelName)
			capability = firstNonEmpty(capabilityFromTaskType(task.Type), capability)
		}
		if matches(modelName, capability, log.ChannelID) {
			filteredLogs = append(filteredLogs, log)
		}
	}
	filteredOrders := make([]model.BillingOrder, 0, len(billingOrders))
	for _, order := range billingOrders {
		modelName, capability := order.Model, normalizeCapability(order.Capability)
		channelID := order.ChannelID
		if task, exists := taskByID[order.TaskID]; exists && task.UserID == order.UserID {
			modelName = firstNonEmpty(task.Model, modelName)
			capability = firstNonEmpty(capabilityFromTaskType(task.Type), capability)
			channelID = firstNonEmpty(channelID, taskChannels[task.ID])
		}
		if matches(modelName, capability, channelID) {
			filteredOrders = append(filteredOrders, order)
		}
	}
	return filteredTasks, filteredLogs, filteredOrders, nil
}

func (s *Service) estimateCallCost(log *model.ApiCallLog) {
	if log.Status == model.ApiCallStatusFailed && !log.UsageAvailable {
		return
	}
	pricing, err := s.repo.ModelPricing(log.ChannelID, log.Model, log.Capability)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return
		}
		return
	}
	cost := int64(0)
	if log.Billable {
		cost = pricing.PerRequestMicros
	}
	cost += log.InputTokens * pricing.InputPerMillionMicros / 1_000_000
	cost += log.OutputTokens * pricing.OutputPerMillionMicros / 1_000_000
	cost += log.CachedTokens * pricing.CachedPerMillionMicros / 1_000_000
	cost += int64(log.MediaCount) * pricing.PerMediaMicros
	cost += int64(log.VideoSeconds) * pricing.PerVideoSecondMicros
	log.EstimatedCostMicros = cost
	log.CostAvailable = true
	log.Currency = pricing.Currency
}

func (s *Service) EnrichAPICallLog(log *model.ApiCallLog, responseBody []byte) {
	if log == nil {
		return
	}
	// A create endpoint has no provider task ID in its URL. Inferring the final
	// path segment here would turn /videos/generations into a bogus task ID when
	// the create request itself failed.
	if log.ProviderRequestID == "" && log.RequestKind != "create" {
		log.ProviderRequestID = providerRequestIDFromPath(log.Path)
	}
	payloads := providerResponsePayloads(responseBody)
	for _, payload := range payloads {
		s.enrichAPICallLogPayload(log, payload)
	}
	s.enrichAPICallLogFailureSummary(log, responseBody)
}

func (s *Service) enrichAPICallLogFailureSummary(log *model.ApiCallLog, responseBody []byte) {
	if log.Status != model.ApiCallStatusFailed || log.StatusCode < 400 {
		return
	}
	userMessage := providerUserFacingErrorMessage(providerHTTPError{
		StatusCode: log.StatusCode,
		Body:       string(responseBody),
	})
	detail := strings.TrimSpace(log.Error)
	if detail == "" || detail == userMessage {
		log.Error = userMessage
		return
	}
	if strings.Contains(detail, userMessage) {
		return
	}
	log.Error = truncateRunes(userMessage+"；上游："+detail, 2_000)
}

func (s *Service) enrichAPICallLogPayload(log *model.ApiCallLog, payload map[string]any) {
	nestedTaskID := ""
	if data, ok := payload["data"].(map[string]any); ok {
		if log.Capability == "video" && strings.Contains(log.Path, "/v1/video/generations") {
			if extracted, err := firstJSONString(data, "task_id", "taskId"); err == nil {
				nestedTaskID = extracted
			}
		}
		for key, value := range data {
			if _, exists := payload[key]; !exists {
				payload[key] = value
			}
		}
	}
	// Responses API 的终态 SSE 把实际响应（包括 usage）放在 response 字段中。
	if response, ok := payload["response"].(map[string]any); ok {
		for key, value := range response {
			if _, exists := payload[key]; !exists {
				payload[key] = value
			}
		}
	}
	if log.Status == model.ApiCallStatusFailed {
		errorCode, errorMessage := providerFailureDetails(payload)
		log.ErrorCode = errorCode
		if errorMessage != "" {
			log.Error = errorMessage
		}
	}
	usage, _ := payload["usage"].(map[string]any)
	if usage != nil {
		inputTokens, inputAvailable := firstInt64Value(usage, "input_tokens", "prompt_tokens")
		outputTokens, outputAvailable := firstInt64Value(usage, "output_tokens", "completion_tokens")
		if inputAvailable {
			log.InputTokens = inputTokens
		}
		if outputAvailable {
			log.OutputTokens = outputTokens
		}
		if inputAvailable || outputAvailable {
			log.UsageAvailable = true
		}
		// 火山方舟视频的输入 Token 恒为 0；查询任务以 completion_tokens 为实际用量，
		// 兼容只返回 total_tokens 的同协议中转实现。
		if log.Capability == "video" && strings.Contains(log.Path, "/contents/generations/tasks") {
			if log.OutputTokens == 0 {
				log.OutputTokens = firstInt64(usage, "total_tokens")
			}
			log.UsageAvailable = log.OutputTokens > 0
		}
		if details, ok := usage["input_tokens_details"].(map[string]any); ok {
			log.CachedTokens = firstInt64(details, "cached_tokens")
		}
		if details, ok := usage["prompt_tokens_details"].(map[string]any); ok && log.CachedTokens == 0 {
			log.CachedTokens = firstInt64(details, "cached_tokens")
		}
	}
	if usageMetadata, ok := payload["usageMetadata"].(map[string]any); ok {
		inputTokens, inputAvailable := firstInt64Value(usageMetadata, "promptTokenCount")
		outputTokens, outputAvailable := firstInt64Value(usageMetadata, "candidatesTokenCount")
		if inputAvailable {
			log.InputTokens = inputTokens
		}
		if outputAvailable {
			log.OutputTokens = outputTokens
		}
		if inputAvailable || outputAvailable {
			log.UsageAvailable = true
		}
		log.CachedTokens = firstInt64(usageMetadata, "cachedContentTokenCount")
	}
	if extracted, err := firstJSONString(payload, "task_id", "id", "request_id", "name"); err == nil {
		log.ProviderRequestID = firstNonEmpty(nestedTaskID, extracted, log.ProviderRequestID)
	} else {
		log.ProviderRequestID = firstNonEmpty(nestedTaskID, log.ProviderRequestID)
	}
	log.ProviderStatus = strings.ToLower(firstNonEmpty(stringField(payload, "status"), log.ProviderStatus))
	if log.ProviderStatus == "failed" || log.ProviderStatus == "cancelled" || log.ProviderStatus == "expired" {
		log.Status = model.ApiCallStatusFailed
		errorCode, errorMessage := providerFailureDetails(payload)
		log.ErrorCode = firstNonEmpty(errorCode, log.ErrorCode)
		log.Error = firstNonEmpty(errorMessage, log.Error)
	}
	if log.Capability == "image" {
		if data, ok := payload["data"].([]any); ok {
			log.MediaCount = len(data)
		} else if images, ok := payload["images"].([]any); ok {
			log.MediaCount = len(images)
		}
	}
}

func providerResponsePayloads(responseBody []byte) []map[string]any {
	if len(responseBody) == 0 {
		return nil
	}
	var payload map[string]any
	if json.Unmarshal(responseBody, &payload) == nil {
		return []map[string]any{payload}
	}

	// 流式文本的用量只出现在最后一个 SSE data 事件中，不能把整段响应当作 JSON。
	result := make([]map[string]any, 0)
	scanner := bufio.NewScanner(bytes.NewReader(responseBody))
	scanner.Buffer(make([]byte, 64<<10), max(len(responseBody)+1, 64<<10))
	dataLines := make([]string, 0, 1)
	flush := func() {
		raw := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		if raw == "" || raw == "[DONE]" {
			return
		}
		var event map[string]any
		if json.Unmarshal([]byte(raw), &event) == nil {
			result = append(result, event)
		}
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	flush()
	return result
}

func providerRequestIDFromPath(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	for index := len(parts) - 1; index >= 0; index-- {
		part := strings.TrimSpace(parts[index])
		if part == "" || part == "content" || part == "download" {
			continue
		}
		if index > 0 && (parts[index-1] == "videos" || parts[index-1] == "tasks") {
			return part
		}
		break
	}
	return ""
}

func firstInt64(values map[string]any, keys ...string) int64 {
	value, _ := firstInt64Value(values, keys...)
	return value
}

func firstInt64Value(values map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		switch value := values[key].(type) {
		case float64:
			return int64(value), true
		case int64:
			return value, true
		case json.Number:
			parsed, err := value.Int64()
			return parsed, err == nil
		}
	}
	return 0, false
}

func (s *Service) recordActivity(userID string, event string, count int) {
	if err := s.repo.RecordUserActivity(userID, event, count, time.Now()); err != nil {
		log.Printf("record user activity failed: event=%s count=%d error=%v", event, count, err)
	}
}
