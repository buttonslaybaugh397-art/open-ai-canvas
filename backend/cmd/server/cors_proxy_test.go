package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORSProxyPreservesPublicPort(t *testing.T) {
	for _, tc := range []struct {
		name, method, origin, forwardedHost, allowlist string
		want                                           int
	}{
		{name: "same origin on mapped port", method: http.MethodPost, origin: "http://canvas.example.com:6868", forwardedHost: "canvas.example.com:6868", want: http.StatusOK},
		{name: "preflight on mapped port", method: http.MethodOptions, origin: "http://canvas.example.com:6868", forwardedHost: "canvas.example.com:6868", want: http.StatusNoContent},
		{name: "ipv4 mapped port", method: http.MethodPost, origin: "http://192.0.2.10:6868", forwardedHost: "192.0.2.10:6868", want: http.StatusOK},
		{name: "ipv6 mapped port", method: http.MethodPost, origin: "http://[2001:db8::1]:6868", forwardedHost: "[2001:db8::1]:6868", want: http.StatusOK},
		{name: "lost proxy port is not same origin", method: http.MethodPost, origin: "http://canvas.example.com:6868", forwardedHost: "canvas.example.com", want: http.StatusForbidden},
		{name: "explicit origin works around lost port", method: http.MethodPost, origin: "http://canvas.example.com:6868", forwardedHost: "canvas.example.com", allowlist: "http://canvas.example.com:6868", want: http.StatusOK},
		{name: "other port remains rejected", method: http.MethodOptions, origin: "http://canvas.example.com:8081", forwardedHost: "canvas.example.com:6868", want: http.StatusForbidden},
		{name: "other domain remains rejected", method: http.MethodPost, origin: "https://other.example.com", forwardedHost: "canvas.example.com:6868", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CANVAS_CORS_ORIGINS", tc.allowlist)
			middleware, err := cors()
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.Use(middleware)
			router.Any("/api/probe", func(c *gin.Context) { c.Status(http.StatusOK) })
			request := httptest.NewRequest(tc.method, "http://backend:8080/api/probe", nil)
			request.Header.Set("Origin", tc.origin)
			request.Header.Set("X-Forwarded-Host", tc.forwardedHost)
			request.Header.Set("Access-Control-Request-Method", http.MethodPost)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d", response.Code, tc.want)
			}
			if tc.want != http.StatusForbidden {
				if response.Header().Get("Access-Control-Allow-Origin") != tc.origin || response.Header().Get("Access-Control-Allow-Credentials") != "true" {
					t.Fatal("successful origin must retain credentialed CORS response headers")
				}
			} else if response.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("rejected origin must not be reflected")
			}
		})
	}
}

func TestNginxForwardsFullAuthorityOnEveryBackendRoute(t *testing.T) {
	content, err := os.ReadFile("../../../nginx.conf")
	if err != nil {
		t.Fatal(err)
	}
	config := string(content)
	if !strings.Contains(config, "map $http_host $canvas_forwarded_host {") || !strings.Contains(config, "default $http_host;") {
		t.Fatal("proxy host must preserve the browser's explicit port")
	}
	if strings.Contains(config, "proxy_set_header Host $host;") || strings.Contains(config, "proxy_set_header X-Forwarded-Host $host;") {
		t.Fatal("bare $host strips the public port")
	}
	if strings.Contains(config, "$http_x_forwarded_host") {
		t.Fatal("do not trust client-supplied forwarded host")
	}
	routes := strings.Count(config, "proxy_pass http://backend:8080;")
	if routes == 0 || strings.Count(config, "proxy_set_header Host $canvas_forwarded_host;") != routes || strings.Count(config, "proxy_set_header X-Forwarded-Host $canvas_forwarded_host;") != routes {
		t.Fatal("every backend route must forward the complete host and port")
	}
}
