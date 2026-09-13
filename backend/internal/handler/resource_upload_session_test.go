package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"infinite-canvas/backend/internal/auth"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"
)

func TestChunkUploadIdentity(t *testing.T) {
	for _, tc := range []struct {
		header, body, want string
		invalid            bool
	}{
		{header: " browser-key ", want: "browser-key"},
		{body: "old-client-key", want: "old-client-key"},
		{header: "same", body: "same", want: "same"},
		{},
		{header: "a", body: "b", invalid: true},
	} {
		got, err := chunkUploadIdentity(tc.header, tc.body)
		if got != tc.want || (err != nil) != tc.invalid {
			t.Fatalf("identity(%q, %q) = %q, %v", tc.header, tc.body, got, err)
		}
	}
}

func TestChunkUploadRetryPreservesConfirmedBytes(t *testing.T) {
	session := &chunkedUploadSession{Dir: t.TempDir(), Size: 7, ChunkCount: 1}
	if err := session.writeChunk(0, strings.NewReader("payload")); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []io.Reader{strings.NewReader("short"), strings.NewReader("oversized"), io.MultiReader(strings.NewReader("payload"), uploadErrorReader{})} {
		if err := session.writeChunk(0, invalid); err == nil {
			t.Fatal("invalid retry should fail")
		}
		data, err := os.ReadFile(session.chunkPath(0))
		if err != nil || string(data) != "payload" {
			t.Fatalf("confirmed chunk changed: %q, %v", data, err)
		}
	}
	if err := session.writeChunk(0, strings.NewReader("payload")); err != nil {
		t.Fatalf("full retry: %v", err)
	}
	entries, err := os.ReadDir(session.Dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "chunk-0" {
		t.Fatalf("temporary files leaked: %v, %v", entries, err)
	}
	if !session.hasAllChunks() {
		t.Fatal("completed chunk missing")
	}
}

type uploadErrorReader struct{}

func (uploadErrorReader) Read([]byte) (int, error) { return 0, errors.New("transport failure") }

func TestChunkUploadConcurrentPartsAndTailLength(t *testing.T) {
	session := &chunkedUploadSession{Dir: t.TempDir(), Size: 2*chunkUploadChunkSize + 17, ChunkCount: 3}
	var workers sync.WaitGroup
	for index := 0; index < 3; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			payload := bytes.Repeat([]byte{byte(index)}, int(session.chunkSizeAt(index)))
			if err := session.writeChunk(index, bytes.NewReader(payload)); err != nil {
				t.Errorf("chunk %d: %v", index, err)
			}
		}(index)
	}
	workers.Wait()
	if !session.hasAllChunks() {
		t.Fatal("parallel chunks missing")
	}
	for index := 0; index < 3; index++ {
		data, err := os.ReadFile(session.chunkPath(index))
		want := bytes.Repeat([]byte{byte(index)}, int(session.chunkSizeAt(index)))
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("chunk %d corrupted: %v", index, err)
		}
	}
}

func TestChunkUploadRejectsClosedAndInvalidSessions(t *testing.T) {
	session := &chunkedUploadSession{Dir: t.TempDir(), Size: 7, ChunkCount: 1}
	for _, index := range []int{-1, 1} {
		if err := session.writeChunk(index, strings.NewReader("payload")); err == nil {
			t.Fatal("invalid index accepted")
		}
	}
	removeChunkSessionFiles(session)
	if err := session.writeChunk(0, strings.NewReader("payload")); err == nil {
		t.Fatal("closed session accepted")
	}
	if _, err := os.Stat(session.Dir); !os.IsNotExist(err) {
		t.Fatalf("closed session directory still exists: %v", err)
	}
}

func TestChunkUploadRouteOwnershipIdentityAndQuota(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.AuthSession{}, &model.SystemSetting{}, &model.UserOSSSetting{}, &model.StorageLocation{}, &model.UserDailyUploadUsage{}, &model.Resource{}, &model.SessionFile{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"owner", "other"} {
		if err := db.Create(&model.User{ID: id, Username: id, Role: model.UserRoleUser, Status: model.UserStatusActive}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.AuthSession{ID: id, UserID: id, TokenHash: auth.HashToken("upload-token"), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := service.New(repository.New(db), t.TempDir())
	previous := runtimeService
	runtimeService = svc
	t.Cleanup(func() { runtimeService = previous; _ = svc.Close() })
	router := gin.New()
	RegisterChunkedUploadRoutes(router.Group("/api"), svc)
	request := func(method, path, body, user, key string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/resources/uploads"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Idempotency-Key", key)
		if user != "" {
			req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: user + ".upload-token"})
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, res.Code, want, res.Body.String())
		}
		return res
	}
	const body = `{"fileName":"test.txt","size":7,"kind":"file"}`
	request(http.MethodPost, "", body, "", "key", http.StatusUnauthorized)
	request(http.MethodPost, "", `{"fileName":"test.txt","size":7,"idempotencyKey":"different"}`, "owner", "key", http.StatusBadRequest)
	var firstID string
	for _, user := range []string{"owner", "owner", "other"} {
		res := request(http.MethodPost, "", body, user, "stable-key", http.StatusOK)
		var started struct {
			Data struct {
				UploadID string `json:"uploadId"`
			} `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &started); err != nil {
			t.Fatal(err)
		}
		id := started.Data.UploadID
		t.Cleanup(func() { dropChunkSession(id) })
		session := takeChunkSession(id)
		if session == nil || session.IdempotencyKey != "stable-key" {
			t.Fatal("header identity was not retained")
		}
		if user == "owner" {
			request(http.MethodPut, "/"+id+"/chunks/0", "payload", "other", "", http.StatusNotFound)
			request(http.MethodPost, "/"+id+"/complete", "", "other", "", http.StatusNotFound)
		}
		request(http.MethodPost, "/"+id+"/complete", "", user, "", http.StatusBadRequest)
		part := request(http.MethodPut, "/"+id+"/chunks/0", "payload", user, "", http.StatusOK)
		if !strings.Contains(part.Header().Get("Server-Timing"), "receive;dur=") {
			t.Fatal("missing part timing")
		}
		request(http.MethodPut, "/"+id+"/chunks/0", "short", user, "", http.StatusBadRequest)
		res = request(http.MethodPost, "/"+id+"/complete", "", user, "", http.StatusOK)
		timings := strings.Join(res.Header().Values("Server-Timing"), ",")
		if !strings.Contains(timings, "merge;dur=") || !strings.Contains(timings, "store;dur=") {
			t.Fatal("missing completion timings")
		}
		var completed struct {
			Data struct {
				Resource model.Resource `json:"resource"`
			} `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &completed); err != nil {
			t.Fatal(err)
		}
		resource := completed.Data.Resource
		if resource.ID == "" || resource.UserID != user || resource.Status != model.ResourceStatusReady {
			t.Fatalf("invalid resource: %#v", resource)
		}
		if firstID == "" {
			firstID = resource.ID
		} else if (resource.ID == firstID) != (user == "owner") {
			t.Fatal("idempotency did not respect user scope")
		}
		_, file, err := svc.OpenResource(user, resource.ID)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(file)
		_ = file.Close()
		if readErr != nil || string(data) != "payload" {
			t.Fatalf("stored content=%q err=%v", data, readErr)
		}
		if takeChunkSession(id) != nil {
			t.Fatal("completed session leaked")
		}
		if _, err := os.Stat(session.Dir); !os.IsNotExist(err) {
			t.Fatalf("session directory leaked: %v", err)
		}
		request(http.MethodPost, "/"+id+"/complete", "", user, "", http.StatusNotFound)
	}
	var count int64
	if err := db.Model(&model.Resource{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("resources=%d err=%v", count, err)
	}
	for _, user := range []string{"owner", "other"} {
		var usage model.UserDailyUploadUsage
		if err := db.First(&usage, "user_id = ?", user).Error; err != nil || usage.Bytes != 7 {
			t.Fatalf("user=%s quota=%d err=%v", user, usage.Bytes, err)
		}
	}
}

func TestUploadTimingReportsSeparateStages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	appendUploadTiming(c, "receive", time.Now().Add(-20*time.Millisecond))
	appendUploadTiming(c, "store", time.Now().Add(-30*time.Millisecond))
	values := recorder.Header().Values("Server-Timing")
	if len(values) != 2 || !strings.HasPrefix(values[0], "receive;dur=") || !strings.HasPrefix(values[1], "store;dur=") {
		t.Fatalf("timing headers = %v", values)
	}
}
