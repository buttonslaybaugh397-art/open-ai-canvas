package updaterclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"infinite-canvas/backend/internal/hostupdate"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
	stream  *http.Client
}

type tokenFileClient struct {
	socketPath    string
	baseURL       string
	tokenFile     string
	fallbackToken string
}

func New(socketPath, token string) *Client {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		DisableKeepAlives: true,
	}
	return &Client{baseURL: "http://unix", token: strings.TrimSpace(token), http: &http.Client{Transport: transport, Timeout: 15 * time.Second}, stream: &http.Client{Transport: transport, Timeout: 45 * time.Minute}}
}

// NewHTTP connects to the Windows local migration helper. It uses the same
// bearer-token protocol as the Unix-socket updater.
func NewHTTP(rawURL, token string) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		baseURL = "http://127.0.0.1:0"
	}
	transport := &http.Transport{DisableKeepAlives: true}
	return &Client{baseURL: baseURL, token: strings.TrimSpace(token), http: &http.Client{Transport: transport, Timeout: 15 * time.Second}, stream: &http.Client{Transport: transport, Timeout: 45 * time.Minute}}
}

// NewTokenFileHTTP delays reading the local helper token until each request.
// This lets Docker Desktop start before the Windows helper creates its token.
func NewTokenFileHTTP(rawURL, tokenFile string) *tokenFileClient {
	return &tokenFileClient{baseURL: rawURL, tokenFile: tokenFile}
}

// NewTokenFile connects to a Unix-socket updater and reads its token for each
// request. This keeps backend startup independent from the updater service
// startup order.
func NewTokenFile(socketPath, tokenFile, fallbackToken string) *tokenFileClient {
	return &tokenFileClient{socketPath: strings.TrimSpace(socketPath), tokenFile: tokenFile, fallbackToken: strings.TrimSpace(fallbackToken)}
}

func (c *tokenFileClient) Status(ctx context.Context) (hostupdate.Status, error) {
	client, err := c.client()
	if err != nil {
		return hostupdate.Status{}, err
	}
	return client.Status(ctx)
}

func (c *tokenFileClient) Check(ctx context.Context) (hostupdate.Status, error) {
	client, err := c.client()
	if err != nil {
		return hostupdate.Status{}, err
	}
	return client.Check(ctx)
}

func (c *tokenFileClient) Start(ctx context.Context, targetVersion string) (hostupdate.Status, error) {
	client, err := c.client()
	if err != nil {
		return hostupdate.Status{}, err
	}
	return client.Start(ctx, targetVersion)
}

func (c *tokenFileClient) Rollback(ctx context.Context, reason string) (hostupdate.Status, error) {
	client, err := c.client()
	if err != nil {
		return hostupdate.Status{}, err
	}
	return client.Rollback(ctx, reason)
}

func (c *tokenFileClient) MigrationExport(ctx context.Context) (hostupdate.Status, error) {
	client, err := c.client()
	if err != nil {
		return hostupdate.Status{}, err
	}
	return client.MigrationExport(ctx)
}

func (c *tokenFileClient) MigrationImport(ctx context.Context, contentLength int64, source io.Reader) (hostupdate.Status, error) {
	client, err := c.client()
	if err != nil {
		return hostupdate.Status{}, err
	}
	return client.MigrationImport(ctx, contentLength, source)
}

func (c *tokenFileClient) OpenMigrationExport(ctx context.Context) (hostupdate.MigrationArchive, io.ReadCloser, error) {
	client, err := c.client()
	if err != nil {
		return hostupdate.MigrationArchive{}, nil, err
	}
	return client.OpenMigrationExport(ctx)
}

func (c *tokenFileClient) client() (*Client, error) {
	data, err := os.ReadFile(c.tokenFile)
	token := strings.TrimSpace(string(data))
	if err != nil || len(token) < 32 {
		if c.fallbackToken == "" {
			if err != nil {
				return nil, fmt.Errorf("读取 Host Updater Token 文件：%w", err)
			}
			return nil, errors.New("Host Updater Token 文件内容无效")
		}
		token = c.fallbackToken
	}
	if len(token) < 32 {
		return nil, errors.New("Host Updater Token 无效")
	}
	if c.socketPath != "" {
		return New(c.socketPath, token), nil
	}
	return NewHTTP(c.baseURL, token), nil
}

func (c *Client) Status(ctx context.Context) (hostupdate.Status, error) {
	return c.request(ctx, http.MethodGet, "/v1/status", nil)
}

func (c *Client) Check(ctx context.Context) (hostupdate.Status, error) {
	return c.request(ctx, http.MethodPost, "/v1/check", struct{}{})
}

func (c *Client) Start(ctx context.Context, targetVersion string) (hostupdate.Status, error) {
	return c.request(ctx, http.MethodPost, "/v1/update", hostupdate.StartRequest{TargetVersion: targetVersion})
}

func (c *Client) Rollback(ctx context.Context, reason string) (hostupdate.Status, error) {
	return c.request(ctx, http.MethodPost, "/v1/rollback", hostupdate.RollbackRequest{Reason: reason})
}

func (c *Client) MigrationExport(ctx context.Context) (hostupdate.Status, error) {
	return c.request(ctx, http.MethodPost, "/v1/migration/export", struct{}{})
}

func (c *Client) MigrationImport(ctx context.Context, contentLength int64, source io.Reader) (hostupdate.Status, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/migration/import", source)
	if err != nil {
		return hostupdate.Status{}, err
	}
	request.ContentLength = contentLength
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/zip")
	response, err := c.stream.Do(request)
	if err != nil {
		return hostupdate.Status{}, fmt.Errorf("连接 Host Updater：%w", err)
	}
	return decodeStatusResponse(response)
}

func (c *Client) OpenMigrationExport(ctx context.Context) (hostupdate.MigrationArchive, io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/migration/download", nil)
	if err != nil {
		return hostupdate.MigrationArchive{}, nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/zip")
	response, err := c.stream.Do(request)
	if err != nil {
		return hostupdate.MigrationArchive{}, nil, fmt.Errorf("连接 Host Updater：%w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, statusErr := decodeStatusResponse(response)
		return hostupdate.MigrationArchive{}, nil, statusErr
	}
	archive := hostupdate.MigrationArchive{
		Checksum: response.Header.Get("X-Migration-SHA256"),
		Size:     response.ContentLength,
		ID:       response.Header.Get("X-Migration-ID"),
		Version:  response.Header.Get("X-Migration-Version"),
	}
	return archive, response.Body, nil
}

func (c *Client) request(ctx context.Context, method, path string, payload any) (hostupdate.Status, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return hostupdate.Status{}, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return hostupdate.Status{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return hostupdate.Status{}, fmt.Errorf("连接 Host Updater：%w", err)
	}
	return decodeStatusResponse(response)
}

func decodeStatusResponse(response *http.Response) (hostupdate.Status, error) {
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return hostupdate.Status{}, errAuthentication
	}
	limited := io.LimitReader(response.Body, 2<<20)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string            `json:"error"`
			Data  hostupdate.Status `json:"data"`
		}
		if err := json.NewDecoder(limited).Decode(&failure); err == nil && failure.Error != "" {
			return failure.Data, fmt.Errorf("Host Updater：%s", failure.Error)
		}
		return hostupdate.Status{}, fmt.Errorf("Host Updater 返回 HTTP %d", response.StatusCode)
	}
	var status hostupdate.Status
	if err := json.NewDecoder(limited).Decode(&status); err != nil {
		return hostupdate.Status{}, fmt.Errorf("解析 Host Updater 响应：%w", err)
	}
	return status, nil
}

var errAuthentication = errors.New("Host Updater 认证失败，Token 不匹配或访问被拒绝")

// ConnectionDetail exposes only known failure categories, never response bodies,
// credentials, URLs, or arbitrary helper errors to the administration page.
func ConnectionDetail(err error) string {
	var pathErr *os.PathError
	var networkErr net.Error
	switch {
	case errors.Is(err, errAuthentication):
		return "Token 认证失败，请核对宿主机服务与 backend 使用的 Token"
	case errors.As(err, &pathErr):
		if errors.Is(err, os.ErrNotExist) {
			return "Token 文件不存在，请检查安装是否完成、目录挂载，以及重启后 Token 是否恢复"
		}
		if errors.Is(err, os.ErrPermission) {
			return "Token 文件不可读，请检查 backend 用户的文件和目录权限"
		}
		return "Token 文件读取失败，请检查文件类型与挂载配置"
	case errors.Is(err, os.ErrNotExist):
		return "Unix Socket 不存在，请检查更新器服务是否运行及 backend 目录挂载"
	case errors.Is(err, os.ErrPermission):
		return "Unix Socket 访问被拒绝，请检查目录、Socket 权限和安全策略"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "连接被拒绝，请检查更新器是否退出或 Socket 是否失效"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &networkErr) && networkErr.Timeout():
		return "更新器响应超时，请检查服务状态与宿主机负载"
	default:
		return "更新器请求失败，请检查 Token 内容、服务日志和连接配置"
	}
}
