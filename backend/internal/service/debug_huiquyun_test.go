package service

import (
	"context"
	"fmt"
	"testing"

	"gorm.io/gorm"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestDebugHuiQuYunReadOnly(t *testing.T) {
	db, err := database.Open(database.Config{Driver: "sqlite", DSN: "file:C:/Users/win10/Downloads/open-ai-canvas-main/open-ai-canvas-main/.local/project-workbench-debug/open_ai_canvas.db?mode=ro&_busy_timeout=5000"})
	if err != nil {
		t.Fatal(err)
	}
	var channels []model.ModelChannel
	if err := db.Where("lower(base_url) LIKE ?", "%bjhuiqu%").Find(&channels).Error; err != nil {
		t.Fatal(err)
	}
	for _, channel := range channels {
		fmt.Printf("CHANNEL id=%s name=%q base=%q enabled=%v apiKeySet=%v\n", channel.ID, channel.Name, channel.BaseURL, channel.Enabled, channel.APIKey != "")
		var models []model.ChannelModel
		if err := db.Where("channel_id = ?", channel.ID).Order("model_key").Find(&models).Error; err != nil {
			t.Fatal(err)
		}
		for _, item := range models {
			fmt.Printf("MODEL key=%q capability=%q protocol=%q enabled=%v priced=%v unit=%d config=%s\n", item.ModelKey, item.Capability, item.Protocol, item.Enabled, item.PriceConfigured, item.UnitPriceMicrocredits, item.CapabilityConfigJSON)
		}
	}
	var logs []model.ApiCallLog
	if err := db.Where("lower(upstream_url) LIKE ? OR lower(model) LIKE ?", "%bjhuiqu%", "%mx933%").Order("created_at desc").Limit(20).Find(&logs).Error; err != nil && err != gorm.ErrRecordNotFound {
		t.Fatal(err)
	}
	for _, item := range logs {
		fmt.Printf("LOG created=%s model=%q path=%q status=%q code=%d request=%s response=%s error=%q\n", item.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), item.Model, item.Path, item.Status, item.StatusCode, item.RequestBody, item.ResponseBody, item.Error)
	}
	for _, channel := range channels {
		if channel.ID != "3a38f3929c9769322f9863c6e1678f9f" {
			continue
		}
		svc := New(repository.New(db), "C:/Users/win10/Downloads/open-ai-canvas-main/open-ai-canvas-main/.local/project-workbench-debug")
		if err := svc.decryptSystemChannelSecrets(&channel); err != nil {
			t.Fatalf("decrypt channel secrets: %v", err)
		}
		headers, err := ParseOutboundHeadersJSON(channel.HeadersJSON)
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := svc.FetchChannelModelCatalog(context.Background(), &model.User{ID: "diagnostic"}, ChannelModelsRequest{BaseURL: channel.BaseURL, APIKey: channel.APIKey, APIFormat: channel.APIFormat, Headers: headers})
		if err != nil {
			t.Fatalf("fetch upstream catalog: %v", err)
		}
		for _, item := range catalog {
			if item.ID == "sd2-mx933-720-5s" || item.ID == "sd2-mx933-720-10s" || item.ID == "sd2-mx933-720-15s" || item.ID == "mj-sd2.0-933-720p-10s" {
				fmt.Printf("CATALOG id=%q endpoints=%v defaults=%+v images=%v min=%v max=%v\n", item.ID, item.SupportedEndpointTypes, item.DefaultParameters, item.SupportsImages, item.MinImages, item.MaxImages)
			}
		}
	}
}
