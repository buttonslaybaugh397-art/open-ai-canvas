package updaterclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"infinite-canvas/backend/internal/hostupdate"
)

func TestTokenFileRecoversWithoutBackendRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	expected := strings.Repeat("a", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+expected {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(hostupdate.Status{Supported: true, Connected: true})
	}))
	defer server.Close()
	client := NewTokenFileHTTP(server.URL, path)
	if _, err := client.Status(context.Background()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing token = %v", err)
	}
	for _, token := range []string{expected, strings.Repeat("b", 32)} {
		expected = token
		if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		status, err := client.Status(context.Background())
		if err != nil || !status.Connected {
			t.Fatalf("restored/rotated token did not reconnect: %v", err)
		}
	}
}

func TestConnectionDetailClassifiesWithoutLeakingSecrets(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&os.PathError{Op: "open", Path: "private-path", Err: os.ErrNotExist}, "Token 文件不存在"},
		{&os.PathError{Op: "open", Path: "private-path", Err: os.ErrPermission}, "Token 文件不可读"},
		{&net.OpError{Op: "dial", Net: "unix", Err: os.ErrNotExist}, "Unix Socket 不存在"},
		{&net.OpError{Op: "dial", Net: "unix", Err: os.ErrPermission}, "Unix Socket 访问被拒绝"},
		{&net.OpError{Op: "dial", Net: "unix", Err: syscall.ECONNREFUSED}, "连接被拒绝"},
		{context.DeadlineExceeded, "响应超时"},
		{errAuthentication, "Token 认证失败"},
		{errors.New("Authorization: Bearer private-secret"), "请求失败"},
	}
	for _, tc := range cases {
		detail := ConnectionDetail(fmt.Errorf("wrapped: %w", tc.err))
		if !strings.Contains(detail, tc.want) || strings.Contains(detail, "private-") {
			t.Fatalf("unexpected diagnostic: %s", detail)
		}
	}
}

func TestStatusAuthenticationDoesNotExposeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"private-secret"}`)
	}))
	defer server.Close()
	_, err := NewHTTP(server.URL, strings.Repeat("a", 32)).Status(context.Background())
	if !errors.Is(err, errAuthentication) || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("unexpected auth error: %v", err)
	}
}
