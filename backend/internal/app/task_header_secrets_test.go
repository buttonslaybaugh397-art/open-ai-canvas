package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/protocol"
)

func TestTaskHeaderSecretsPersistedProtocolRequest(t *testing.T) {
	const apiKey = "synthetic-protocol-api-key"
	const response = `{"ok":true}`
	headers := []OutboundHeader{
		{Name: "x-api-key", Value: "synthetic-protocol-header-key"},
		{Name: "X-Plugin-Token", Value: "synthetic-protocol-plugin-token"},
		{Name: "User-Agent", Value: "SyntheticProtocolTest/1.0"},
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/header-smoke" {
			t.Error("protocol request used an unexpected method or path")
		}
		for _, header := range headers {
			if r.Header.Get(header.Name) != header.Value {
				t.Errorf("upstream did not receive the original %s value", header.Name)
			}
		}
		if r.Header.Get("Authorization") != "Bearer "+apiKey {
			t.Error("upstream did not receive the original API key")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("protocol JSON content type was not preserved")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer upstream.Close()

	input, err := normalizeTaskInput(map[string]any{
		"mode": "text",
		"config": providerConfig{
			BaseURL: upstream.URL + "/v1", APIFormat: "openai", APIKey: apiKey,
			Headers: headers, AllowLocalChannel: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{dataDir: t.TempDir()}
	if err := svc.protectTaskSecrets(input); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(svc.dataDir, "task-input.json")
	if err := os.WriteFile(path, []byte(taskHeaderSecretsJSON(t, input)), 0o600); err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(persisted), encryptedSettingPrefix) || strings.Contains(string(persisted), apiKey) {
		t.Fatal("persisted protocol input was not protected")
	}
	for _, header := range headers {
		if strings.Contains(string(persisted), header.Value) {
			t.Fatal("persisted protocol input contains a plaintext header")
		}
	}
	reloaded := &Service{dataDir: svc.dataDir}
	plain, err := reloaded.decryptTaskInputJSON(string(persisted))
	if err != nil {
		t.Fatal(err)
	}
	var execution canvasGenerationInput
	if err := json.Unmarshal([]byte(plain), &execution); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = withProviderOutboundPolicy(ctx, execution.Config)
	body, err := executeProtocolRequest(ctx, execution.Config, protocol.RequestSpec{
		Method: http.MethodPost, Path: "/header-smoke", ContentType: "application/json",
		Body: map[string]any{"prompt": "synthetic smoke"},
		Auth: protocol.ManifestAuth{Type: "bearer", Field: "apiKey"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != response || calls.Load() != 1 {
		t.Fatal("persisted protocol request did not complete exactly once")
	}
}

func TestTaskHeaderSecretsRoundTrip(t *testing.T) {
	config := providerConfig{
		BaseURL: "https://example.com", Model: "text-model",
		APIKey: "test-api-secret", SecretKey: "test-secret-key",
		RunningHubWalletKey: "test-wallet-secret", RunningHubUploadKey: "test-upload-secret",
		Headers: []OutboundHeader{
			{Name: "x-api-key", Value: "test-header-secret"},
			{Name: "X-Plugin-Token", Value: "  test-plugin-secret  "},
			{Name: "X-Sentinel", Value: "system"},
			{Name: "X-Empty", Value: ""},
			{Name: "User-Agent", Value: "TestPlugin/1.0"},
		},
	}
	input, err := normalizeTaskInput(map[string]any{
		"mode": "text", "config": config, "resourceId": "resource-1",
		"metadata": map[string]any{"nodeId": "node-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := taskHeaderSecretsJSON(t, input)
	svc := &Service{dataDir: t.TempDir()}
	if err := svc.protectTaskSecrets(input); err != nil {
		t.Fatal(err)
	}
	stored := taskHeaderSecretsJSON(t, input)
	for _, secret := range []string{config.APIKey, config.SecretKey, config.RunningHubWalletKey, config.RunningHubUploadKey} {
		if strings.Contains(stored, secret) {
			t.Fatal("persisted input contains a plaintext credential")
		}
	}
	headers := input["config"].(map[string]any)["headers"].([]any)
	for index, expected := range config.Headers {
		header := headers[index].(map[string]any)
		if header["name"] != expected.Name {
			t.Fatalf("header %d name changed", index)
		}
		protected, ok := header["value"].(string)
		if !ok {
			t.Fatalf("header %d value is not a string", index)
		}
		if expected.Value != "" && (!strings.HasPrefix(protected, encryptedSettingPrefix) || strings.Contains(stored, taskHeaderSecretsJSON(t, expected.Value))) {
			t.Fatalf("header %d value was not protected", index)
		}
	}
	if err := svc.protectTaskSecrets(input); err != nil {
		t.Fatal(err)
	}
	if stored != taskHeaderSecretsJSON(t, input) {
		t.Fatal("repeated protection changed ciphertext")
	}
	// Reloading with the same data directory must use the existing .settings-key.
	reloaded := &Service{dataDir: svc.dataDir}
	plain, err := reloaded.decryptTaskInputJSON(stored)
	if err != nil {
		t.Fatal(err)
	}
	if plain != before {
		t.Fatal("task input did not round trip")
	}
	var execution canvasGenerationInput
	if err := json.Unmarshal([]byte(plain), &execution); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(execution.Config, config) {
		t.Fatal("execution provider config changed")
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	ApplyOutboundHeaders(req, execution.Config.Headers)
	if req.Header.Get("X-Api-Key") != config.Headers[0].Value {
		t.Fatal("execution did not receive the decrypted header")
	}
	for _, raw := range []string{before, stored, plain} {
		output := taskForOutput(model.Task{InputJSON: raw}).InputJSON
		var public map[string]any
		if err := json.Unmarshal([]byte(output), &public); err != nil {
			t.Fatal(err)
		}
		if _, exists := public["config"]; exists || strings.Contains(output, encryptedSettingPrefix) || strings.Contains(output, "test-header-secret") {
			t.Fatal("public task output exposes provider credentials")
		}
		if public["mode"] != "text" || public["resourceId"] != "resource-1" || len(public) != 3 {
			t.Fatal("public task metadata was not preserved")
		}
	}
}

func TestTaskHeaderSecretsPreserveAPIKeySentinelsAndAbsentHeaders(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`{"config":null}`,
		`{"config":{"apiKey":"system"}}`,
		`{"config":{"apiKey":"","headers":null}}`,
		`{"config":{"apiKey":"system","headers":[]}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var input map[string]any
			if err := json.Unmarshal([]byte(raw), &input); err != nil {
				t.Fatal(err)
			}
			svc := &Service{dataDir: t.TempDir()}
			before := taskHeaderSecretsJSON(t, input)
			if err := svc.protectTaskSecrets(input); err != nil {
				t.Fatal(err)
			}
			if before != taskHeaderSecretsJSON(t, input) {
				t.Fatal("empty headers or API key sentinel changed")
			}
			plain, err := svc.decryptTaskInputJSON(before)
			if err != nil || plain != before {
				t.Fatalf("unchanged input failed to round trip: %v", err)
			}
		})
	}
}

func TestTaskHeaderSecretsIgnoreNonConfigHeaders(t *testing.T) {
	input, err := normalizeTaskInput(map[string]any{
		"headers": []OutboundHeader{{Name: "X-Example", Value: "root-example"}},
		"config": map[string]any{
			"headers": []OutboundHeader{{Name: "x-api-key", Value: "test-header-secret"}},
			"workflowJson": map[string]any{
				"config": map[string]any{"headers": "workflow-field"},
			},
		},
		"agentRequests": map[string]any{
			"responses": map[string]any{
				"tools": []any{map[string]any{
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"headers": map[string]any{"type": "object"},
							"config": map[string]any{
								"headers": []OutboundHeader{{Name: "X-Example", Value: encryptedSettingPrefix + "schema-example"}},
							},
						},
					},
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{dataDir: t.TempDir()}
	before := taskHeaderSecretsJSON(t, input)
	toolsBefore := taskHeaderSecretsJSON(t, input["agentRequests"])
	workflowBefore := taskHeaderSecretsJSON(t, input["config"].(map[string]any)["workflowJson"])
	for range 2 {
		if err := svc.protectTaskSecrets(input); err != nil {
			t.Fatal(err)
		}
		if toolsBefore != taskHeaderSecretsJSON(t, input["agentRequests"]) || workflowBefore != taskHeaderSecretsJSON(t, input["config"].(map[string]any)["workflowJson"]) {
			t.Fatal("non-provider headers were modified")
		}
		if input["headers"].([]any)[0].(map[string]any)["value"] != "root-example" {
			t.Fatal("root non-config headers were encrypted")
		}
	}
	plain, err := svc.decryptTaskInputJSON(taskHeaderSecretsJSON(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if plain != before {
		t.Fatal("non-provider headers were modified during decryption")
	}
}

func TestTaskHeaderSecretsRejectInvalidStructure(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"config string", `{"config":"invalid"}`},
		{"config array", `{"config":[]}`},
		{"config alias", `{"Config":{"headers":[{"name":"x-api-key","value":"test-secret"}]}}`},
		{"headers alias", `{"config":{"Headers":[{"name":"x-api-key","value":"test-secret"}]}}`},
		{"headers map", `{"config":{"headers":{"x-api-key":"test-secret"}}}`},
		{"headers string", `{"config":{"headers":"test-secret"}}`},
		{"headers number", `{"config":{"headers":1}}`},
		{"null entry", `{"config":{"headers":[null]}}`},
		{"string entry", `{"config":{"headers":["test-secret"]}}`},
		{"array entry", `{"config":{"headers":[["x-api-key","test-secret"]]}}`},
		{"missing name", `{"config":{"headers":[{"value":"test-secret"}]}}`},
		{"missing value", `{"config":{"headers":[{"name":"x-api-key"}]}}`},
		{"null name", `{"config":{"headers":[{"name":null,"value":"test-secret"}]}}`},
		{"null value", `{"config":{"headers":[{"name":"x-api-key","value":null}]}}`},
		{"numeric name", `{"config":{"headers":[{"name":1,"value":"test-secret"}]}}`},
		{"numeric value", `{"config":{"headers":[{"name":"x-api-key","value":1}]}}`},
		{"object value", `{"config":{"headers":[{"name":"x-api-key","value":{"token":"test-secret"}}]}}`},
		{"value alias", `{"config":{"headers":[{"name":"x-api-key","Value":"test-secret"}]}}`},
		{"extra field", `{"config":{"headers":[{"name":"x-api-key","value":"test-secret","Value":"shadow-secret"}]}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var input map[string]any
			if err := json.Unmarshal([]byte(test.raw), &input); err != nil {
				t.Fatal(err)
			}
			svc := &Service{dataDir: t.TempDir()}
			for _, operation := range []func() error{
				func() error { return svc.protectTaskSecrets(input) },
				func() error { return svc.decryptTaskSecrets(input) },
				func() error {
					_, err := svc.decryptTaskInputJSON(test.raw)
					return err
				},
			} {
				err := operation()
				var appErr *AppError
				if !errors.As(err, &appErr) || appErr.Status != http.StatusBadRequest {
					t.Fatalf("expected input validation error, got %v", err)
				}
				if strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "shadow-secret") {
					t.Fatal("validation error contains a header value")
				}
			}
		})
	}
}

func TestTaskHeaderSecretsValidatePlaintextHeaders(t *testing.T) {
	for _, test := range []struct {
		name    string
		headers []OutboundHeader
	}{
		{"bad name", []OutboundHeader{{Name: "X Invalid", Value: "test-secret"}}},
		{"blank name", []OutboundHeader{{Value: "test-secret"}}},
		{"control character", []OutboundHeader{{Name: "X-Token", Value: "test\r\nsecret"}}},
		{"duplicate", []OutboundHeader{{Name: "X-Token", Value: "first"}, {Name: "x-token", Value: "second"}}},
		{"managed header", []OutboundHeader{{Name: "Authorization", Value: "test-secret"}}},
		{"long value", []OutboundHeader{{Name: "X-Token", Value: strings.Repeat("x", 4097)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := &Service{dataDir: t.TempDir()}
			for _, encrypted := range []bool{false, true} {
				input, err := normalizeTaskInput(map[string]any{"config": providerConfig{Headers: test.headers}})
				if err != nil {
					t.Fatal(err)
				}
				if encrypted {
					for _, raw := range input["config"].(map[string]any)["headers"].([]any) {
						header := raw.(map[string]any)
						header["value"], err = svc.encryptSettingSecret(header["value"].(string))
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := svc.protectTaskSecrets(input); err == nil {
					t.Fatal("protection accepted invalid plaintext headers")
				}
				if _, err := svc.decryptTaskInputJSON(taskHeaderSecretsJSON(t, input)); err == nil {
					t.Fatal("decryption accepted invalid plaintext headers")
				}
			}
		})
	}
}

func TestTaskHeaderSecretsAcceptMaximumPlaintextValue(t *testing.T) {
	svc := &Service{dataDir: t.TempDir()}
	input, err := normalizeTaskInput(map[string]any{
		"config": providerConfig{Headers: []OutboundHeader{{Name: "X-Token", Value: strings.Repeat("x", 4096)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := taskHeaderSecretsJSON(t, input)
	for range 2 {
		if err := svc.protectTaskSecrets(input); err != nil {
			t.Fatal(err)
		}
	}
	plain, err := svc.decryptTaskInputJSON(taskHeaderSecretsJSON(t, input))
	if err != nil || plain != before {
		t.Fatalf("maximum-length header failed to round trip: %v", err)
	}
}

func TestTaskHeaderSecretsRejectCorruptCiphertext(t *testing.T) {
	svc := &Service{dataDir: t.TempDir()}
	valid, err := svc.encryptSettingSecret("test-header-secret")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(valid, encryptedSettingPrefix))
	if err != nil {
		t.Fatal(err)
	}
	payload[len(payload)-1] ^= 1
	other := &Service{dataDir: t.TempDir()}
	wrongKey, err := other.encryptSettingSecret("test-header-secret")
	if err != nil {
		t.Fatal(err)
	}
	for name, secret := range map[string]string{
		"invalid encoding": encryptedSettingPrefix + "!",
		"truncated":        encryptedSettingPrefix + base64.RawStdEncoding.EncodeToString([]byte("short")),
		"tampered":         encryptedSettingPrefix + base64.RawStdEncoding.EncodeToString(payload),
		"wrong key":        wrongKey,
	} {
		t.Run(name, func(t *testing.T) {
			input, err := normalizeTaskInput(map[string]any{
				"config": providerConfig{Headers: []OutboundHeader{{Name: "x-api-key", Value: secret}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			before := taskHeaderSecretsJSON(t, input)
			if err := svc.protectTaskSecrets(input); err == nil {
				t.Fatal("protection accepted corrupt ciphertext")
			} else if strings.Contains(err.Error(), secret) {
				t.Fatal("protection error contains ciphertext")
			}
			if before != taskHeaderSecretsJSON(t, input) {
				t.Fatal("failed protection changed headers")
			}
			plain, err := svc.decryptTaskInputJSON(before)
			if err == nil || plain != "" {
				t.Fatal("decryption did not fail closed")
			}
		})
	}
}

func TestTaskHeaderSecretsDecryptEscapedCiphertext(t *testing.T) {
	svc := &Service{dataDir: t.TempDir()}
	secret, err := svc.encryptSettingSecret("test-header-secret")
	if err != nil {
		t.Fatal(err)
	}
	raw := taskHeaderSecretsJSON(t, map[string]any{
		"config": providerConfig{Headers: []OutboundHeader{{Name: "x-api-key", Value: secret}}},
	})
	raw = strings.ReplaceAll(raw, encryptedSettingPrefix, `\u0065nc:v1:`)
	plain, err := svc.decryptTaskInputJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var input canvasGenerationInput
	if err := json.Unmarshal([]byte(plain), &input); err != nil {
		t.Fatal(err)
	}
	if input.Config.Headers[0].Value != "test-header-secret" {
		t.Fatal("escaped ciphertext was not decrypted")
	}
}

func taskHeaderSecretsJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
