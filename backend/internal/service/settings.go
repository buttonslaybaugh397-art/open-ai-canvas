package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const ossSettingKey = "oss"
const encryptedSettingPrefix = "enc:v1:"

const (
	aliyunOSSProvider  = "aliyun"
	tencentCOSProvider = "tencent"
	qiniuKodoProvider  = "qiniu"
)

type OSSSettingRequest struct {
	Enabled         bool   `json:"enabled"`
	Provider        string `json:"provider"`
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint"`
	CDNBaseURL      string `json:"cdnBaseUrl"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
	PublicBaseURL   string `json:"publicBaseUrl"`
	PathPrefix      string `json:"pathPrefix"`
}

type PublicOSSSetting struct {
	Enabled            bool                                `json:"enabled"`
	Provider           string                              `json:"provider"`
	Region             string                              `json:"region"`
	Endpoint           string                              `json:"endpoint"`
	CDNBaseURL         string                              `json:"cdnBaseUrl"`
	Bucket             string                              `json:"bucket"`
	AccessKeyID        string                              `json:"accessKeyId"`
	HasAccessKeySecret bool                                `json:"hasAccessKeySecret"`
	PublicBaseURL      string                              `json:"publicBaseUrl"`
	PathPrefix         string                              `json:"pathPrefix"`
	UpdatedBy          string                              `json:"updatedBy"`
	CreatedAt          time.Time                           `json:"createdAt"`
	UpdatedAt          time.Time                           `json:"updatedAt"`
	ProviderSettings   map[string]PublicOSSProviderSetting `json:"providerSettings,omitempty"`
}

type PublicOSSProviderSetting struct {
	Region             string `json:"region"`
	Endpoint           string `json:"endpoint"`
	CDNBaseURL         string `json:"cdnBaseUrl"`
	Bucket             string `json:"bucket"`
	AccessKeyID        string `json:"accessKeyId"`
	HasAccessKeySecret bool   `json:"hasAccessKeySecret"`
	PathPrefix         string `json:"pathPrefix"`
}

type ossSettingValue struct {
	Enabled         bool   `json:"enabled"`
	Provider        string `json:"provider"`
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint"`
	CDNBaseURL      string `json:"cdnBaseUrl"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
	PublicBaseURL   string `json:"publicBaseUrl"`
	PathPrefix      string `json:"pathPrefix"`
	// 平台切换云厂商后仍需读取历史资源，因此仅归档非当前厂商的访问密钥。
	ArchivedCredentials map[string]ossProviderCredentials `json:"archivedCredentials,omitempty"`
	// 平台切换云厂商后保留各供应商的完整配置，避免覆盖其他供应商。
	ArchivedSettings map[string]ossProviderSetting `json:"archivedSettings,omitempty"`
}

type ossProviderCredentials struct {
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
}

type ossProviderSetting struct {
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint"`
	CDNBaseURL      string `json:"cdnBaseUrl"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
	PathPrefix      string `json:"pathPrefix"`
}

func (s *Service) AdminOSSSetting(actor *model.User) (*PublicOSSSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	setting, value, err := s.readOSSSetting()
	if err != nil {
		return nil, err
	}
	public := publicOSSSetting(setting, value)
	return &public, nil
}

func (s *Service) UpdateOSSSetting(actor *model.User, req OSSSettingRequest) (*PublicOSSSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	currentSetting, currentValue, err := s.readOSSSetting()
	if err != nil {
		return nil, err
	}
	next, err := ossSettingFromRequest(req, currentValue)
	if err != nil {
		return nil, err
	}
	if !next.Enabled {
		if next.PublicBaseURL == "" {
			return nil, BadAuthRequest("服务器本地存储需要填写服务器访问地址")
		}
		if _, err := validatePublicResourceBaseURL(next.PublicBaseURL); err != nil {
			return nil, fmt.Errorf("服务器访问地址无效：%w", err)
		}
	}
	next = archiveOSSProviderSettings(next, currentValue)
	stored, err := s.encryptOSSSettingSecrets(next)
	if err != nil {
		return nil, err
	}
	valueJSON, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	setting := model.SystemSetting{
		Key:       ossSettingKey,
		ValueJSON: string(valueJSON),
		UpdatedBy: actor.ID,
	}
	if currentSetting != nil {
		setting.CreatedAt = currentSetting.CreatedAt
	}
	if err := s.repo.SaveSystemSetting(&setting); err != nil {
		return nil, err
	}
	public := publicOSSSetting(&setting, next)
	return &public, nil
}

func (s *Service) UserOSSSetting(actor *model.User) (*PublicOSSSetting, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	setting, value, err := s.readUserOSSSetting(actor.ID)
	if err != nil {
		return nil, err
	}
	public := publicUserOSSSetting(setting, value)
	return &public, nil
}

func (s *Service) UpdateUserOSSSetting(actor *model.User, req OSSSettingRequest) (*PublicOSSSetting, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	_, currentValue, err := s.readUserOSSSetting(actor.ID)
	if err != nil {
		return nil, err
	}
	next, err := ossSettingFromRequest(req, currentValue)
	if err != nil {
		return nil, err
	}
	stored, err := s.encryptOSSSettingSecrets(next)
	if err != nil {
		return nil, err
	}
	valueJSON, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	// 配置按版本追加而不是覆盖，资源会固定引用创建时的版本。
	setting := model.UserOSSSetting{ID: newID(), UserID: actor.ID, Enabled: next.Enabled, ValueJSON: string(valueJSON)}
	if err := s.repo.CreateUserOSSSetting(&setting); err != nil {
		return nil, err
	}
	public := publicUserOSSSetting(&setting, next)
	return &public, nil
}

func (s *Service) readOSSSetting() (*model.SystemSetting, ossSettingValue, error) {
	setting, err := s.repo.SystemSetting(ossSettingKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, defaultOSSSetting(), nil
	}
	if err != nil {
		return nil, ossSettingValue{}, err
	}
	value := defaultOSSSetting()
	if strings.TrimSpace(setting.ValueJSON) != "" {
		if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
			return nil, ossSettingValue{}, errors.New("平台 OSS 配置格式无效")
		}
	}
	needsMigration, err := s.decryptOSSSettingSecrets(&value)
	if err != nil {
		return nil, ossSettingValue{}, err
	}
	if needsMigration {
		migrated, err := s.encryptOSSSettingSecrets(value)
		if err != nil {
			return nil, ossSettingValue{}, err
		}
		encoded, err := json.Marshal(migrated)
		if err != nil {
			return nil, ossSettingValue{}, err
		}
		setting.ValueJSON = string(encoded)
		if err := s.repo.SaveSystemSetting(setting); err != nil {
			return nil, ossSettingValue{}, err
		}
	}
	return setting, normalizeOSSSetting(value), nil
}

func (s *Service) readUserOSSSetting(userID string) (*model.UserOSSSetting, ossSettingValue, error) {
	setting, err := s.repo.LatestUserOSSSetting(userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, defaultOSSSetting(), nil
	}
	if err != nil {
		return nil, ossSettingValue{}, err
	}
	value, err := s.userOSSSettingValue(setting)
	return setting, value, err
}

func (s *Service) readUserOSSSettingByID(userID string, id string) (*model.UserOSSSetting, ossSettingValue, error) {
	setting, err := s.repo.UserOSSSettingForUser(userID, id)
	if err != nil {
		return nil, ossSettingValue{}, err
	}
	value, err := s.userOSSSettingValue(setting)
	return setting, value, err
}

func (s *Service) userOSSSettingValue(setting *model.UserOSSSetting) (ossSettingValue, error) {
	value := defaultOSSSetting()
	if strings.TrimSpace(setting.ValueJSON) != "" {
		if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
			return ossSettingValue{}, errors.New("用户 OSS 配置格式无效")
		}
	}
	if _, err := s.decryptOSSSettingSecrets(&value); err != nil {
		return ossSettingValue{}, err
	}
	value.Enabled = setting.Enabled
	return normalizeOSSSetting(value), nil
}

func (s *Service) encryptOSSSettingSecrets(value ossSettingValue) (ossSettingValue, error) {
	var err error
	value.AccessKeySecret, err = s.encryptSettingSecret(value.AccessKeySecret)
	if err != nil {
		return ossSettingValue{}, err
	}
	value.ArchivedCredentials = cloneOSSProviderCredentials(value.ArchivedCredentials)
	for provider, credentials := range value.ArchivedCredentials {
		credentials.AccessKeySecret, err = s.encryptSettingSecret(credentials.AccessKeySecret)
		if err != nil {
			return ossSettingValue{}, err
		}
		value.ArchivedCredentials[provider] = credentials
	}
	value.ArchivedSettings = cloneOSSProviderSettings(value.ArchivedSettings)
	for provider, setting := range value.ArchivedSettings {
		setting.AccessKeySecret, err = s.encryptSettingSecret(setting.AccessKeySecret)
		if err != nil {
			return ossSettingValue{}, err
		}
		value.ArchivedSettings[provider] = setting
	}
	return value, nil
}

func (s *Service) decryptOSSSettingSecrets(value *ossSettingValue) (bool, error) {
	needsMigration := value.AccessKeySecret != "" && !strings.HasPrefix(value.AccessKeySecret, encryptedSettingPrefix)
	secret, err := s.decryptSettingSecret(value.AccessKeySecret)
	if err != nil {
		return false, err
	}
	value.AccessKeySecret = secret
	value.ArchivedCredentials = cloneOSSProviderCredentials(value.ArchivedCredentials)
	for provider, credentials := range value.ArchivedCredentials {
		if credentials.AccessKeySecret != "" && !strings.HasPrefix(credentials.AccessKeySecret, encryptedSettingPrefix) {
			needsMigration = true
		}
		credentials.AccessKeySecret, err = s.decryptSettingSecret(credentials.AccessKeySecret)
		if err != nil {
			return false, err
		}
		value.ArchivedCredentials[provider] = credentials
	}
	value.ArchivedSettings = cloneOSSProviderSettings(value.ArchivedSettings)
	for provider, setting := range value.ArchivedSettings {
		if setting.AccessKeySecret != "" && !strings.HasPrefix(setting.AccessKeySecret, encryptedSettingPrefix) {
			needsMigration = true
		}
		setting.AccessKeySecret, err = s.decryptSettingSecret(setting.AccessKeySecret)
		if err != nil {
			return false, err
		}
		value.ArchivedSettings[provider] = setting
	}
	return needsMigration, nil
}

func (s *Service) encryptSettingSecret(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	key, err := s.settingsEncryptionKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(value), nil)
	return encryptedSettingPrefix + base64.RawStdEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

func (s *Service) decryptSettingSecret(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedSettingPrefix) {
		return value, nil
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, encryptedSettingPrefix))
	if err != nil {
		return "", errors.New("OSS 密钥密文格式无效")
	}
	key, err := s.settingsEncryptionKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", errors.New("OSS 密钥密文长度无效")
	}
	plaintext, err := gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("OSS 密钥解密失败，请检查存储加密密钥")
	}
	return string(plaintext), nil
}

func (s *Service) settingsEncryptionKey() ([]byte, error) {
	path := filepath.Join(s.dataDir, ".settings-key")
	if data, err := os.ReadFile(path); err == nil && len(data) == 32 {
		return data, nil
	}
	if err := os.MkdirAll(s.dataDir, 0o750); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("读取存储加密密钥失败：%w", readErr)
		}
		if len(data) != 32 {
			return nil, errors.New("存储加密密钥长度无效")
		}
		return data, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Service) protectTaskSecrets(value interface{}) error {
	switch item := value.(type) {
	case map[string]interface{}:
		for key, child := range item {
			if isTaskSecretField(key) {
				secret, _ := child.(string)
				if secret != "" && secret != "system" && !strings.HasPrefix(secret, encryptedSettingPrefix) {
					encrypted, err := s.encryptSettingSecret(secret)
					if err != nil {
						return err
					}
					item[key] = encrypted
				}
				continue
			}
			if err := s.protectTaskSecrets(child); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, child := range item {
			if err := s.protectTaskSecrets(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) decryptTaskInputJSON(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" || !strings.Contains(raw, encryptedSettingPrefix) {
		return raw, nil
	}
	var input interface{}
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return "", err
	}
	if err := s.decryptTaskSecrets(input); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(input)
	return string(encoded), err
}

func (s *Service) decryptTaskSecrets(value interface{}) error {
	switch item := value.(type) {
	case map[string]interface{}:
		for key, child := range item {
			if isTaskSecretField(key) {
				secret, _ := child.(string)
				if strings.HasPrefix(secret, encryptedSettingPrefix) {
					plain, err := s.decryptSettingSecret(secret)
					if err != nil {
						return err
					}
					item[key] = plain
				}
				continue
			}
			if err := s.decryptTaskSecrets(child); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, child := range item {
			if err := s.decryptTaskSecrets(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func isTaskSecretField(key string) bool {
	switch key {
	case "apiKey", "secretKey", "runningHubWalletApiKey", "runningHubUploadApiKey":
		return true
	default:
		return false
	}
}

func ossSettingFromRequest(req OSSSettingRequest, current ossSettingValue) (ossSettingValue, error) {
	next := normalizeOSSSetting(ossSettingValue{
		Enabled:         req.Enabled,
		Provider:        strings.TrimSpace(req.Provider),
		Region:          strings.TrimSpace(req.Region),
		Endpoint:        strings.TrimRight(strings.TrimSpace(req.Endpoint), "/"),
		CDNBaseURL:      strings.TrimRight(strings.TrimSpace(req.CDNBaseURL), "/"),
		Bucket:          strings.TrimSpace(req.Bucket),
		AccessKeyID:     strings.TrimSpace(req.AccessKeyID),
		AccessKeySecret: strings.TrimSpace(req.AccessKeySecret),
		PublicBaseURL:   strings.TrimRight(strings.TrimSpace(req.PublicBaseURL), "/"),
		PathPrefix:      strings.Trim(strings.TrimSpace(req.PathPrefix), "/"),
	})
	if next.Provider != aliyunOSSProvider && next.Provider != tencentCOSProvider && next.Provider != qiniuKodoProvider {
		return next, BadAuthRequest("仅支持阿里云 OSS、腾讯云 COS 和七牛云 Kodo")
	}
	current = normalizeOSSSetting(current)
	// 不同云厂商的密钥不能复用；只有继续使用同一厂商时，留空才表示保留原密钥。
	if next.Provider != current.Provider {
		next = restoreArchivedOSSProviderSetting(next, current)
	}
	if !next.Enabled && next.Provider == current.Provider && !hasOSSProviderSetting(next) {
		// 切换到服务器本地时，前端不会提交隐藏的云存储字段；停用只改变启用状态，
		// 必须保留当前供应商配置，方便之后重新启用时直接恢复。
		next.Region = current.Region
		next.Endpoint = current.Endpoint
		next.CDNBaseURL = current.CDNBaseURL
		next.Bucket = current.Bucket
		next.AccessKeyID = current.AccessKeyID
		next.AccessKeySecret = current.AccessKeySecret
		next.PathPrefix = current.PathPrefix
	}
	if next.AccessKeySecret == "" {
		if next.Provider == current.Provider {
			next.AccessKeySecret = current.AccessKeySecret
		} else if archived, ok := archivedOSSProviderSetting(current, next.Provider); ok {
			next.AccessKeySecret = archived.AccessKeySecret
		}
	}
	if next.Enabled {
		if next.Bucket == "" {
			return next, BadAuthRequest("请填写对象存储 Bucket")
		}
		if next.Endpoint == "" {
			if next.Provider == tencentCOSProvider {
				return next, BadAuthRequest("请填写腾讯云 COS Region 或 Endpoint")
			}
			if next.Provider == qiniuKodoProvider {
				return next, BadAuthRequest("请填写七牛云 Kodo 上传 Endpoint")
			}
			return next, BadAuthRequest("请填写阿里云 OSS Endpoint")
		}
		if _, err := ValidateOutboundURL(next.Endpoint); err != nil {
			return next, err
		}
		if next.CDNBaseURL != "" {
			if _, err := ossCDNBaseURL(next.CDNBaseURL); err != nil {
				return next, BadAuthRequest(err.Error())
			}
			if _, err := ValidateOutboundURL(next.CDNBaseURL); err != nil {
				return next, err
			}
		}
		if next.AccessKeyID == "" {
			return next, BadAuthRequest("请填写访问密钥 AccessKey")
		}
		if next.AccessKeySecret == "" {
			return next, BadAuthRequest("请填写访问密钥 SecretKey")
		}
	}
	return next, nil
}

func archiveOSSProviderSettings(next ossSettingValue, current ossSettingValue) ossSettingValue {
	next = normalizeOSSSetting(next)
	current = normalizeOSSSetting(current)
	next.ArchivedSettings = cloneOSSProviderSettings(current.ArchivedSettings)
	next.ArchivedCredentials = cloneOSSProviderCredentials(current.ArchivedCredentials)
	if current.Provider != next.Provider && hasOSSProviderSetting(current) {
		if next.ArchivedSettings == nil {
			next.ArchivedSettings = make(map[string]ossProviderSetting)
		}
		next.ArchivedSettings[current.Provider] = ossProviderSettingFromValue(current)
		if next.ArchivedCredentials == nil {
			next.ArchivedCredentials = make(map[string]ossProviderCredentials)
		}
		next.ArchivedCredentials[current.Provider] = ossProviderCredentials{AccessKeyID: current.AccessKeyID, AccessKeySecret: current.AccessKeySecret}
	}
	delete(next.ArchivedSettings, next.Provider)
	delete(next.ArchivedCredentials, next.Provider)
	return next
}

// 保留旧函数名，兼容已有测试和历史内部调用。
func archiveOSSProviderCredentials(next ossSettingValue, current ossSettingValue) ossSettingValue {
	return archiveOSSProviderSettings(next, current)
}

func cloneOSSProviderCredentials(source map[string]ossProviderCredentials) map[string]ossProviderCredentials {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]ossProviderCredentials, len(source))
	for provider, credentials := range source {
		cloned[strings.ToLower(strings.TrimSpace(provider))] = ossProviderCredentials{
			AccessKeyID:     strings.TrimSpace(credentials.AccessKeyID),
			AccessKeySecret: strings.TrimSpace(credentials.AccessKeySecret),
		}
	}
	return cloned
}

func cloneOSSProviderSettings(source map[string]ossProviderSetting) map[string]ossProviderSetting {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]ossProviderSetting, len(source))
	for provider, setting := range source {
		cloned[strings.ToLower(strings.TrimSpace(provider))] = normalizeOSSProviderSetting(setting)
	}
	return cloned
}

func normalizeOSSProviderSetting(value ossProviderSetting) ossProviderSetting {
	value.Region = strings.TrimSpace(value.Region)
	value.Endpoint = strings.TrimRight(strings.TrimSpace(value.Endpoint), "/")
	value.CDNBaseURL = strings.TrimRight(strings.TrimSpace(value.CDNBaseURL), "/")
	value.Bucket = strings.TrimSpace(value.Bucket)
	value.AccessKeyID = strings.TrimSpace(value.AccessKeyID)
	value.AccessKeySecret = strings.TrimSpace(value.AccessKeySecret)
	value.PathPrefix = strings.Trim(strings.TrimSpace(value.PathPrefix), "/")
	return value
}

func ossProviderSettingFromValue(value ossSettingValue) ossProviderSetting {
	return normalizeOSSProviderSetting(ossProviderSetting{
		Region: value.Region, Endpoint: value.Endpoint, CDNBaseURL: value.CDNBaseURL, Bucket: value.Bucket,
		AccessKeyID: value.AccessKeyID, AccessKeySecret: value.AccessKeySecret, PathPrefix: value.PathPrefix,
	})
}

func ossSettingValueFromProviderSetting(provider string, value ossProviderSetting) ossSettingValue {
	value = normalizeOSSProviderSetting(value)
	return ossSettingValue{
		Provider: provider, Region: value.Region, Endpoint: value.Endpoint, CDNBaseURL: value.CDNBaseURL,
		Bucket: value.Bucket, AccessKeyID: value.AccessKeyID, AccessKeySecret: value.AccessKeySecret, PathPrefix: value.PathPrefix,
	}
}

func hasOSSProviderSetting(value ossSettingValue) bool {
	return value.Region != "" || value.Endpoint != "" || value.CDNBaseURL != "" || value.Bucket != "" ||
		value.AccessKeyID != "" || value.AccessKeySecret != "" || value.PathPrefix != ""
}

func archivedOSSProviderSetting(setting ossSettingValue, provider string) (ossProviderSetting, bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if value, ok := setting.ArchivedSettings[provider]; ok {
		return normalizeOSSProviderSetting(value), true
	}
	if credentials, ok := setting.ArchivedCredentials[provider]; ok {
		return normalizeOSSProviderSetting(ossProviderSetting{AccessKeyID: credentials.AccessKeyID, AccessKeySecret: credentials.AccessKeySecret}), true
	}
	return ossProviderSetting{}, false
}

func restoreArchivedOSSProviderSetting(next ossSettingValue, current ossSettingValue) ossSettingValue {
	archived, ok := archivedOSSProviderSetting(current, next.Provider)
	if !ok {
		return next
	}
	next.Region = firstNonEmpty(next.Region, archived.Region)
	next.Endpoint = firstNonEmpty(next.Endpoint, archived.Endpoint)
	next.CDNBaseURL = firstNonEmpty(next.CDNBaseURL, archived.CDNBaseURL)
	next.Bucket = firstNonEmpty(next.Bucket, archived.Bucket)
	next.PathPrefix = firstNonEmpty(next.PathPrefix, archived.PathPrefix)
	next.AccessKeyID = firstNonEmpty(next.AccessKeyID, archived.AccessKeyID)
	return next
}

func normalizeOSSSetting(value ossSettingValue) ossSettingValue {
	value.Provider = strings.ToLower(strings.TrimSpace(value.Provider))
	if value.Provider == "" {
		value.Provider = aliyunOSSProvider
	}
	value.Region = strings.TrimSpace(value.Region)
	value.Endpoint = strings.TrimRight(strings.TrimSpace(value.Endpoint), "/")
	if value.Provider == tencentCOSProvider && value.Endpoint == "" && value.Region != "" {
		value.Endpoint = "https://cos." + value.Region + ".myqcloud.com"
	}
	value.CDNBaseURL = strings.TrimRight(strings.TrimSpace(value.CDNBaseURL), "/")
	value.Bucket = strings.TrimSpace(value.Bucket)
	value.AccessKeyID = strings.TrimSpace(value.AccessKeyID)
	value.AccessKeySecret = strings.TrimSpace(value.AccessKeySecret)
	value.PublicBaseURL = strings.TrimRight(strings.TrimSpace(value.PublicBaseURL), "/")
	value.PathPrefix = strings.Trim(strings.TrimSpace(value.PathPrefix), "/")
	value.ArchivedCredentials = cloneOSSProviderCredentials(value.ArchivedCredentials)
	value.ArchivedSettings = cloneOSSProviderSettings(value.ArchivedSettings)
	return value
}

func defaultOSSSetting() ossSettingValue {
	return ossSettingValue{Provider: aliyunOSSProvider}
}

func publicOSSSetting(setting *model.SystemSetting, value ossSettingValue) PublicOSSSetting {
	result := PublicOSSSetting{
		Enabled:            value.Enabled,
		Provider:           value.Provider,
		Region:             value.Region,
		Endpoint:           value.Endpoint,
		CDNBaseURL:         value.CDNBaseURL,
		Bucket:             value.Bucket,
		AccessKeyID:        value.AccessKeyID,
		HasAccessKeySecret: strings.TrimSpace(value.AccessKeySecret) != "",
		PublicBaseURL:      value.PublicBaseURL,
		PathPrefix:         value.PathPrefix,
		ProviderSettings:   publicOSSProviderSettings(value),
	}
	if setting != nil {
		result.UpdatedBy = setting.UpdatedBy
		result.CreatedAt = setting.CreatedAt
		result.UpdatedAt = setting.UpdatedAt
	}
	return result
}

func publicOSSProviderSettings(value ossSettingValue) map[string]PublicOSSProviderSetting {
	result := make(map[string]PublicOSSProviderSetting)
	for provider, setting := range value.ArchivedSettings {
		result[provider] = PublicOSSProviderSetting{
			Region: setting.Region, Endpoint: setting.Endpoint, CDNBaseURL: setting.CDNBaseURL,
			Bucket: setting.Bucket, AccessKeyID: setting.AccessKeyID,
			HasAccessKeySecret: setting.AccessKeySecret != "", PathPrefix: setting.PathPrefix,
		}
	}
	if value.Provider != "" {
		result[value.Provider] = PublicOSSProviderSetting{
			Region: value.Region, Endpoint: value.Endpoint, CDNBaseURL: value.CDNBaseURL,
			Bucket: value.Bucket, AccessKeyID: value.AccessKeyID,
			HasAccessKeySecret: value.AccessKeySecret != "", PathPrefix: value.PathPrefix,
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func publicUserOSSSetting(setting *model.UserOSSSetting, value ossSettingValue) PublicOSSSetting {
	result := PublicOSSSetting{
		Enabled:            value.Enabled,
		Provider:           value.Provider,
		Region:             value.Region,
		Endpoint:           value.Endpoint,
		CDNBaseURL:         value.CDNBaseURL,
		Bucket:             value.Bucket,
		AccessKeyID:        value.AccessKeyID,
		HasAccessKeySecret: strings.TrimSpace(value.AccessKeySecret) != "",
		PublicBaseURL:      value.PublicBaseURL,
		PathPrefix:         value.PathPrefix,
	}
	if setting != nil {
		result.UpdatedBy = setting.UserID
		result.CreatedAt = setting.CreatedAt
		result.UpdatedAt = setting.UpdatedAt
	}
	return result
}
