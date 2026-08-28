package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestChannelsSystemReturnsEnabledSystemChannels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.AuthSession{},
		&model.UserIdentity{},
		&model.ModelChannel{},
		&model.ChannelModel{},
		&model.LogicalModel{},
		&model.LogicalModelRevision{},
		&model.LogicalModelRoute{},
		&model.SystemSetting{},
	); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	user := model.User{ID: "user-1", Username: "test-user", DisplayName: "Test User", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token := "session-token"
	hash := sha256.Sum256([]byte(token))
	session := model.AuthSession{ID: "session-1", UserID: user.ID, TokenHash: hex.EncodeToString(hash[:]), ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	channel := model.ModelChannel{ID: "channel-1", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Managed Video", BaseURL: "https://upstream.example/v1", APIFormat: "openai", ModelsJSON: `[]`, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	channelModel := model.ChannelModel{ID: "channel-model-1", ChannelID: channel.ID, ModelKey: "video-model", DisplayName: "Video Model", Capability: "video", Protocol: model.ChannelInterfaceHuiQuYunVideo, BillingMode: "fixed_request", PriceConfigured: true, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&channelModel).Error; err != nil {
		t.Fatal(err)
	}

	svc := service.New(repository.New(db), t.TempDir())
	router := gin.New()
	api := router.Group("/api")
	RegisterAuthRoutes(api, svc)

	request := httptest.NewRequest(http.MethodGet, "/api/channels/system", nil)
	request.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: session.ID + "." + token})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /channels/system status = %d, body = %s", response.Code, response.Body.String())
	}

	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Channels []struct {
				ID         string   `json:"id"`
				Models     []string `json:"models"`
				ModelCosts []struct {
					Model string `json:"model"`
				} `json:"modelCosts"`
			} `json:"channels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 {
		t.Fatalf("response code = %d, body = %s", envelope.Code, response.Body.String())
	}
	if len(envelope.Data.Channels) != 1 || envelope.Data.Channels[0].ID != channel.ID {
		t.Fatalf("channels = %#v", envelope.Data.Channels)
	}
	if len(envelope.Data.Channels[0].Models) != 1 || envelope.Data.Channels[0].Models[0] != channelModel.ModelKey {
		t.Fatalf("system channel models = %#v", envelope.Data.Channels[0].Models)
	}
	if len(envelope.Data.Channels[0].ModelCosts) != 1 || envelope.Data.Channels[0].ModelCosts[0].Model != channelModel.ModelKey {
		t.Fatalf("system channel model costs = %#v", envelope.Data.Channels[0].ModelCosts)
	}
}
