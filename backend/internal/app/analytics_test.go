package app

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestBuildAnalyticsOverviewUsesTaskFactsForGeneratedMedia(t *testing.T) {
	from := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 2)
	started := from.Add(time.Hour)
	completed := started.Add(12 * time.Second)
	tasks := []model.Task{
		{ID: "video-task", UserID: "user-1", Type: "canvas_video", Status: model.TaskStatusSucceeded, Model: "video-model", InputJSON: "{\"config\":{\"videoSeconds\":\"8\"}}", ResultJSON: "{\"video\":{\"url\":\"https://cdn.example/video.mp4\"}}", StartedAt: &started, CompletedAt: &completed, CreatedAt: started},
		{ID: "image-task", UserID: "user-1", Type: "canvas_image", Status: model.TaskStatusSucceeded, Model: "image-model", InputJSON: "{\"config\":{\"count\":\"4\"}}", ResultJSON: "{\"images\":[{\"url\":\"https://cdn.example/1.png\"},{\"url\":\"https://cdn.example/2.png\"}]}", CreatedAt: started},
		{ID: "failed-task", UserID: "user-2", Type: "canvas_audio", Status: model.TaskStatusFailed, Model: "audio-model", CreatedAt: started},
	}
	logs := []model.ApiCallLog{{ID: "video-root", UserID: "user-1", TaskID: "video-task", Model: "video-model", Capability: "video", RequestKind: "create", Status: model.ApiCallStatusSucceeded, PollCount: 6, VideoSeconds: 8, MediaCount: 1, UsageAvailable: true, InputTokens: 10, OutputTokens: 20, CachedTokens: 3, CostAvailable: true, EstimatedCostMicros: 900, Currency: "USD", DurationMs: 12_000, CreatedAt: started}}
	billing := []model.BillingOrder{{ID: "billing-1", UserID: "user-1", TaskID: "video-task", Model: "video-model", Capability: "video", Status: model.BillingStatusSettled, ActualAmountMicrocredits: 2_500_000}}
	users := []model.User{{ID: "user-1", DisplayName: "测试用户一"}, {ID: "user-2", DisplayName: "测试用户二"}}

	result := buildAnalyticsOverview(repository.AnalyticsFilter{From: from, To: to}, tasks, tasks, logs, logs, billing, nil, users)
	if result.KPI.VideoSeconds != 8 {
		t.Fatalf("video seconds = %d, want task duration 8 once despite %d polls", result.KPI.VideoSeconds, logs[0].PollCount)
	}
	if result.KPI.MediaCount != 3 {
		t.Fatalf("media count = %d, want one video plus two image outputs", result.KPI.MediaCount)
	}
	if result.KPI.GeneratedImages != 2 || result.KPI.GeneratedVideos != 1 || result.KPI.GeneratedAudio != 0 {
		t.Fatalf("generated media breakdown = images:%d videos:%d audio:%d", result.KPI.GeneratedImages, result.KPI.GeneratedVideos, result.KPI.GeneratedAudio)
	}
	if result.KPI.VideoTasks != 1 || result.KPI.ImageTasks != 1 || result.KPI.AudioTasks != 1 {
		t.Fatalf("capability counts = video:%d image:%d audio:%d", result.KPI.VideoTasks, result.KPI.ImageTasks, result.KPI.AudioTasks)
	}
	if result.KPI.SucceededTasks != 2 || result.KPI.FailedTasks != 1 || result.KPI.TaskSuccessRate != 200.0/3.0 {
		t.Fatalf("task status KPI = succeeded:%d failed:%d rate:%f", result.KPI.SucceededTasks, result.KPI.FailedTasks, result.KPI.TaskSuccessRate)
	}
	if result.KPI.InputTokens != 10 || result.KPI.OutputTokens != 20 || result.KPI.CachedTokens != 3 {
		t.Fatalf("token KPI = %d/%d/%d", result.KPI.InputTokens, result.KPI.OutputTokens, result.KPI.CachedTokens)
	}
	if result.KPI.SucceededRequests != 1 || result.KPI.FailedRequests != 0 {
		t.Fatalf("request KPI = succeeded:%d failed:%d", result.KPI.SucceededRequests, result.KPI.FailedRequests)
	}
	if result.KPI.CreditsConsumedMicrocredits != 2_500_000 {
		t.Fatalf("credits consumed = %d", result.KPI.CreditsConsumedMicrocredits)
	}
	if len(result.Users) != 2 {
		t.Fatalf("user rows = %#v", result.Users)
	}
	user := result.Users[0]
	if user.UserID != "user-1" || user.VideoSeconds != 8 || user.MediaCount != 3 || user.Requests != 1 || user.CreditsConsumedMicrocredits != 2_500_000 {
		t.Fatalf("user analytics = %#v", user)
	}
	if user.AverageTaskDurationMs != 12_000 || user.InputTokens != 10 || user.OutputTokens != 20 || user.CachedTokens != 3 {
		t.Fatalf("user duration and usage = %#v", user)
	}
	if user.GeneratedImages != 2 || user.GeneratedVideos != 1 || len(user.Models) != 2 || len(user.Daily) != 1 {
		t.Fatalf("user media/model/daily detail = %#v", user)
	}
}

func TestBuildAnalyticsOverviewReconcilesUserModelChannelAndDailyBreakdowns(t *testing.T) {
	from := time.Date(2026, time.September, 5, 0, 0, 0, 0, time.UTC)
	dayOne := from.Add(2 * time.Hour)
	dayTwo := from.AddDate(0, 0, 1).Add(3 * time.Hour)
	videoDone := dayOne.Add(10 * time.Second)
	imageDone := dayTwo.Add(4 * time.Second)
	firstActive := dayOne.Add(-time.Hour)
	lastActive := dayOne.Add(4 * time.Hour)
	tasks := []model.Task{
		{ID: "video", UserID: "user-1", Type: "canvas_video", Status: model.TaskStatusSucceeded, Model: "logical-video", InputJSON: "{\"durationSeconds\":10}", StartedAt: &dayOne, CompletedAt: &videoDone, CreatedAt: dayOne},
		{ID: "audio-running", UserID: "user-1", Type: "canvas_audio", Status: model.TaskStatusRunning, Model: "logical-audio", CreatedAt: dayTwo},
		{ID: "image", UserID: "user-2", Type: "canvas_image", Status: model.TaskStatusSucceeded, Model: "logical-image", InputJSON: "{\"count\":1}", ResultJSON: "{\"image\":{\"url\":\"https://cdn.example/image.png\"}}", StartedAt: &dayTwo, CompletedAt: &imageDone, CreatedAt: dayTwo},
	}
	logs := []model.ApiCallLog{
		{ID: "video-create", UserID: "user-1", TaskID: "video", ChannelID: "channel-a", Model: "provider-video", Capability: "video", RequestKind: "create", Status: model.ApiCallStatusSucceeded, DurationMs: 1_000, UsageAvailable: true, InputTokens: 11, OutputTokens: 22, CachedTokens: 3, CostAvailable: true, EstimatedCostMicros: 400, Currency: "USD", CreatedAt: dayOne},
		{ID: "image-create", UserID: "user-2", TaskID: "image", ChannelID: "channel-b", Model: "provider-image", Capability: "image", RequestKind: "create", Status: model.ApiCallStatusFailed, DurationMs: 4_000, CostAvailable: true, EstimatedCostMicros: 600, Currency: "USD", CreatedAt: dayTwo},
	}
	billing := []model.BillingOrder{
		{ID: "bill-video", UserID: "user-1", TaskID: "video", ChannelID: "channel-a", Model: "provider-video", Capability: "video", Status: model.BillingStatusSettled, ActualAmountMicrocredits: 1_500_000, SettledAt: &videoDone, CreatedAt: dayOne},
		{ID: "bill-image", UserID: "user-2", TaskID: "image", ChannelID: "channel-b", Model: "provider-image", Capability: "image", Status: model.BillingStatusSettled, ActualAmountMicrocredits: 500_000, SettledAt: &imageDone, CreatedAt: dayTwo},
	}
	activities := []model.UserDailyActivity{
		{UserID: "user-1", Day: from, LoginCount: 2, AgentMessageCount: 3, CanvasActive: true, FirstActiveAt: &firstActive, LastActiveAt: &lastActive},
		{UserID: "user-2", Day: from.AddDate(0, 0, 1), LoginCount: 1, AssetCount: 2, ResourceCount: 4},
	}
	users := []model.User{{ID: "user-1", DisplayName: "用户一"}, {ID: "user-2", DisplayName: "用户二"}}

	result := buildAnalyticsOverview(repository.AnalyticsFilter{From: from, To: from.AddDate(0, 0, 2)}, tasks, tasks, logs, logs, billing, activities, users)
	if result.KPI.GenerationTasks != 3 || result.KPI.SucceededTasks != 2 || result.KPI.RunningTasks != 1 {
		t.Fatalf("task KPI = %#v", result.KPI)
	}
	if result.KPI.SucceededRequests != 1 || result.KPI.FailedRequests != 1 || result.KPI.GeneratedImages != 1 || result.KPI.GeneratedVideos != 1 || result.KPI.VideoSeconds != 10 {
		t.Fatalf("request/output KPI = %#v", result.KPI)
	}

	userOne := analyticsUserByID(result.Users, "user-1")
	if userOne == nil || userOne.LoginCount != 2 || userOne.Tasks != 2 || userOne.RunningTasks != 1 || userOne.VideoSeconds != 10 || userOne.CreditsConsumedMicrocredits != 1_500_000 {
		t.Fatalf("user one = %#v", userOne)
	}
	if len(userOne.Models) != 2 || len(userOne.Channels) != 2 || len(userOne.Daily) != 2 || userOne.FirstActiveAt == nil || userOne.LastActiveAt == nil {
		t.Fatalf("user one details = %#v", userOne)
	}
	userOneChannelA := analyticsUserChannelByID(userOne.Channels, "channel-a")
	if userOneChannelA == nil || userOneChannelA.Tasks != 1 || userOneChannelA.Requests != 1 || userOneChannelA.VideoSeconds != 10 || userOneChannelA.CreditsConsumedMicrocredits != 1_500_000 {
		t.Fatalf("user one channel a = %#v", userOneChannelA)
	}
	userTwo := analyticsUserByID(result.Users, "user-2")
	if userTwo == nil || userTwo.LoginCount != 1 || userTwo.GeneratedImages != 1 || userTwo.FailedRequests != 1 || userTwo.CreditsConsumedMicrocredits != 500_000 {
		t.Fatalf("user two = %#v", userTwo)
	}

	channelA := analyticsChannelByID(result.Channels, "channel-a")
	channelB := analyticsChannelByID(result.Channels, "channel-b")
	local := analyticsChannelByID(result.Channels, "")
	if channelA == nil || channelA.Tasks != 1 || channelA.Requests != 1 || channelA.VideoSeconds != 10 || channelA.CreditsConsumedMicrocredits != 1_500_000 {
		t.Fatalf("channel a = %#v", channelA)
	}
	if channelB == nil || channelB.Tasks != 1 || channelB.FailedRequests != 1 || channelB.GeneratedImages != 1 || channelB.CreditsConsumedMicrocredits != 500_000 {
		t.Fatalf("channel b = %#v", channelB)
	}
	if local == nil || local.Tasks != 1 || local.Requests != 0 {
		t.Fatalf("local channel = %#v", local)
	}

	assertAnalyticsReconciles(t, result)
}

func TestAnalyticsTaskChannelsDoNotReplaceKnownChannelWithEmptyOrForeignFacts(t *testing.T) {
	now := time.Date(2026, time.September, 5, 0, 0, 0, 0, time.UTC)
	settledAt := now.Add(4 * time.Minute)
	tasks := []model.Task{
		{ID: "with-order", UserID: "user-1"},
		{ID: "with-log", UserID: "user-1"},
	}
	logs := []model.ApiCallLog{
		{TaskID: "with-order", UserID: "user-1", ChannelID: "channel-log", Status: model.ApiCallStatusSucceeded, CreatedAt: now},
		{TaskID: "with-order", UserID: "user-1", ChannelID: "", Status: model.ApiCallStatusSucceeded, CreatedAt: now.Add(time.Minute)},
		{TaskID: "with-log", UserID: "foreign-user", ChannelID: "foreign-channel", Status: model.ApiCallStatusSucceeded, CreatedAt: now.Add(3 * time.Minute)},
		{TaskID: "with-log", UserID: "user-1", ChannelID: "channel-failed", Status: model.ApiCallStatusFailed, CreatedAt: now.Add(time.Minute)},
		{TaskID: "with-log", UserID: "user-1", ChannelID: "channel-success", Status: model.ApiCallStatusSucceeded, CreatedAt: now.Add(2 * time.Minute)},
	}
	orders := []model.BillingOrder{
		{TaskID: "with-order", UserID: "user-1", ChannelID: "channel-billed", SettledAt: &settledAt},
		{TaskID: "with-log", UserID: "foreign-user", ChannelID: "foreign-billed", SettledAt: &settledAt},
	}

	channels := analyticsTaskChannels(tasks, logs, orders)
	if channels["with-order"] != "channel-billed" {
		t.Fatalf("billed task channel = %q, want channel-billed", channels["with-order"])
	}
	if channels["with-log"] != "channel-success" {
		t.Fatalf("logged task channel = %q, want channel-success", channels["with-log"])
	}
}

func TestFilterAnalyticsFactsUsesTaskModelAndFinalChannelAttribution(t *testing.T) {
	now := time.Date(2026, time.September, 5, 0, 0, 0, 0, time.UTC)
	tasks := []model.Task{{ID: "task-1", UserID: "user-1", Type: "canvas_video", Model: "logical-video"}}
	logs := []model.ApiCallLog{
		{ID: "failed-a", TaskID: "task-1", UserID: "user-1", ChannelID: "channel-a", Model: "provider-video-a", Capability: "video", Status: model.ApiCallStatusFailed, CreatedAt: now},
		{ID: "success-b", TaskID: "task-1", UserID: "user-1", ChannelID: "channel-b", Model: "provider-video-b", Capability: "video", Status: model.ApiCallStatusSucceeded, CreatedAt: now.Add(time.Minute)},
	}
	billing := []model.BillingOrder{{ID: "bill-b", TaskID: "task-1", UserID: "user-1", ChannelID: "channel-b", Model: "provider-video-b", Capability: "video", Status: model.BillingStatusSettled}}

	filteredTasks, filteredLogs, filteredBilling, err := filterAnalyticsFacts(repository.AnalyticsFilter{Model: "logical-video", ChannelID: "channel-a", Capability: "video"}, tasks, tasks, logs, billing)
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredTasks) != 0 || len(filteredLogs) != 1 || filteredLogs[0].ID != "failed-a" || len(filteredBilling) != 0 {
		t.Fatalf("channel-a facts = tasks:%#v logs:%#v billing:%#v", filteredTasks, filteredLogs, filteredBilling)
	}
	filteredTasks, filteredLogs, filteredBilling, err = filterAnalyticsFacts(repository.AnalyticsFilter{Model: "logical-video", ChannelID: "channel-b", Capability: "video"}, tasks, tasks, logs, billing)
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredTasks) != 1 || len(filteredLogs) != 1 || filteredLogs[0].ID != "success-b" || len(filteredBilling) != 1 {
		t.Fatalf("channel-b facts = tasks:%#v logs:%#v billing:%#v", filteredTasks, filteredLogs, filteredBilling)
	}
}

func TestBuildAnalyticsUsersCountsLoginOnlyDayAsPersonalActivity(t *testing.T) {
	from := time.Date(2026, time.September, 5, 0, 0, 0, 0, time.UTC)
	loginAt := from.Add(9 * time.Hour)
	activity := model.UserDailyActivity{
		UserID:     "login-only",
		Day:        from,
		LoginCount: 2,
		CreatedAt:  loginAt,
		UpdatedAt:  loginAt.Add(time.Hour),
	}

	result := buildAnalyticsOverview(
		repository.AnalyticsFilter{From: from, To: from.AddDate(0, 0, 1)},
		nil, nil, nil, nil, nil, []model.UserDailyActivity{activity},
		[]model.User{{ID: "login-only", DisplayName: "仅登录用户"}},
	)
	user := analyticsUserByID(result.Users, "login-only")
	if user == nil || user.LoginCount != 2 || user.ActiveDays != 1 || user.FirstActiveAt == nil || user.LastActiveAt == nil {
		t.Fatalf("login-only user analytics = %#v", user)
	}
	if result.KPI.ActiveUsers != 1 || result.KPI.DAU != 1 || len(user.Daily) != 1 {
		t.Fatalf("login-only activity totals = kpi:%#v user:%#v", result.KPI, user)
	}
}

func analyticsUserByID(rows []AnalyticsUserRow, userID string) *AnalyticsUserRow {
	for index := range rows {
		if rows[index].UserID == userID {
			return &rows[index]
		}
	}
	return nil
}

func analyticsChannelByID(rows []AnalyticsChannelRow, channelID string) *AnalyticsChannelRow {
	for index := range rows {
		if rows[index].ChannelID == channelID {
			return &rows[index]
		}
	}
	return nil
}

func analyticsUserChannelByID(rows []AnalyticsUserChannelRow, channelID string) *AnalyticsUserChannelRow {
	for index := range rows {
		if rows[index].ChannelID == channelID {
			return &rows[index]
		}
	}
	return nil
}

func assertAnalyticsReconciles(t *testing.T, result *AnalyticsOverview) {
	t.Helper()
	userTasks, userRequests, userSeconds, userCredits := 0, 0, 0, int64(0)
	for _, row := range result.Users {
		userTasks += row.Tasks
		userRequests += row.Requests
		userSeconds += row.VideoSeconds
		userCredits += row.CreditsConsumedMicrocredits
	}
	channelTasks, channelRequests, channelSeconds, channelCredits := 0, 0, 0, int64(0)
	for _, row := range result.Channels {
		channelTasks += row.Tasks
		channelRequests += row.Requests
		channelSeconds += row.VideoSeconds
		channelCredits += row.CreditsConsumedMicrocredits
	}
	if userTasks != result.KPI.GenerationTasks || channelTasks != result.KPI.GenerationTasks || userRequests != result.KPI.UpstreamRequests || channelRequests != result.KPI.UpstreamRequests || userSeconds != result.KPI.VideoSeconds || channelSeconds != result.KPI.VideoSeconds || userCredits != result.KPI.CreditsConsumedMicrocredits || channelCredits != result.KPI.CreditsConsumedMicrocredits {
		t.Fatalf("analytics did not reconcile: kpi=%#v users=%d/%d/%d/%d channels=%d/%d/%d/%d", result.KPI, userTasks, userRequests, userSeconds, userCredits, channelTasks, channelRequests, channelSeconds, channelCredits)
	}
}

func TestTaskGeneratedVideoSecondsCountsLocalTaskWithoutRequestLog(t *testing.T) {
	task := model.Task{Type: "canvas_video", Status: model.TaskStatusSucceeded, InputJSON: "{\"metadata\":{\"durationSeconds\":15}}"}
	if got := taskGeneratedVideoSeconds(task); got != 15 {
		t.Fatalf("taskGeneratedVideoSeconds() = %d, want 15", got)
	}
}

func TestGeneratedVideoSecondsFallsBackToCanonicalTaskLogOnce(t *testing.T) {
	task := model.Task{ID: "legacy-video", Type: "canvas_video", Status: model.TaskStatusSucceeded, InputJSON: "{}"}
	logs := []model.ApiCallLog{
		{TaskID: task.ID, Capability: "video", RequestKind: "create", VideoSeconds: 10},
		{TaskID: task.ID, Capability: "video", RequestKind: "poll", VideoSeconds: 10},
	}
	seconds := generatedVideoSecondsByTask([]model.Task{task}, logs)
	if got := seconds[task.ID]; got != 10 {
		t.Fatalf("generated video seconds = %d, want one 10-second output despite repeated task logs", got)
	}
}

func TestGeneratedVideoSecondsPrefersTaskSnapshotOverLogs(t *testing.T) {
	task := model.Task{ID: "current-video", Type: "canvas_video", Status: model.TaskStatusSucceeded, InputJSON: "{\"config\":{\"videoSeconds\":\"12\"}}"}
	logs := []model.ApiCallLog{{TaskID: task.ID, Capability: "video", VideoSeconds: 15}}
	seconds := generatedVideoSecondsByTask([]model.Task{task}, logs)
	if got := seconds[task.ID]; got != 12 {
		t.Fatalf("generated video seconds = %d, want task snapshot 12", got)
	}
}

func TestGeneratedVideoSecondsExcludesFailedTaskLog(t *testing.T) {
	task := model.Task{ID: "failed-video", Type: "canvas_video", Status: model.TaskStatusFailed, InputJSON: "{}"}
	seconds := generatedVideoSecondsByTask([]model.Task{task}, []model.ApiCallLog{{TaskID: task.ID, Capability: "video", VideoSeconds: 10}})
	if got := seconds[task.ID]; got != 0 {
		t.Fatalf("failed task video seconds = %d, want 0", got)
	}
}

func TestBuildAnalyticsModelsJoinsRequestAndBillingFactsByTask(t *testing.T) {
	task := model.Task{ID: "video-task", UserID: "user-1", Type: "canvas_video", Status: model.TaskStatusSucceeded, Model: "logical-video-model"}
	logs := []model.ApiCallLog{{TaskID: task.ID, UserID: task.UserID, Model: "provider-video-model", Capability: "video", Status: model.ApiCallStatusSucceeded}}
	billing := []model.BillingOrder{{TaskID: task.ID, UserID: task.UserID, Model: "channel-video-model", Capability: "video", Status: model.BillingStatusSettled, ActualAmountMicrocredits: 3_000_000}}

	rows := buildAnalyticsModels([]model.Task{task}, logs, billing, map[string]int{task.ID: 10})
	if len(rows) != 1 {
		t.Fatalf("model rows = %#v, want one task-aligned row", rows)
	}
	row := rows[0]
	if row.Model != task.Model || row.Tasks != 1 || row.Requests != 1 || row.CreditsConsumedMicrocredits != 3_000_000 || row.VideoSeconds != 10 {
		t.Fatalf("joined model analytics = %#v", row)
	}
}
