package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const ossSettingKey = "oss"
const encryptedSettingPrefix = "enc:v1:"
const defaultOSSPathPrefix = "open-ai-canvas"

const (
	aliyunOSSProvider  = "aliyun"
	tencentCOSProvider = "tencent"
	qiniuKodoProvider  = "qiniu"
	s3Provider         = "s3"
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
	S3Preset        string `json:"s3Preset"`
	PathStyle       bool   `json:"pathStyle"`
	SessionToken    string `json:"sessionToken"`
	AllowUserS3     bool   `json:"allowUserS3"`
}

type PublicOSSSetting struct {
	Enabled                 bool                                `json:"enabled"`
	Provider                string                              `json:"provider"`
	Region                  string                              `json:"region"`
	Endpoint                string                              `json:"endpoint"`
	CDNBaseURL              string                              `json:"cdnBaseUrl"`
	Bucket                  string                              `json:"bucket"`
	AccessKeyID             string                              `json:"accessKeyId"`
	HasAccessKeySecret      bool                                `json:"hasAccessKeySecret"`
	PublicBaseURL           string                              `json:"publicBaseUrl"`
	PathPrefix              string                              `json:"pathPrefix"`
	S3Preset                string                              `json:"s3Preset"`
	PathStyle               bool                                `json:"pathStyle"`
	HasSessionToken         bool                                `json:"hasSessionToken"`
	StorageLocationID       string                              `json:"storageLocationId"`
	TestedAt                *time.Time                          `json:"testedAt,omitempty"`
	TestedDigest            string                              `json:"testedDigest,omitempty"`
	HistoryCount            int64                               `json:"historyCount"`
	ReferencedResourceCount int64                               `json:"referencedResourceCount"`
	AllowUserS3             bool                                `json:"allowUserS3"`
	ProviderSettings        map[string]PublicOSSProviderSetting `json:"providerSettings,omitempty"`
	UpdatedBy               string                              `json:"updatedBy"`
	CreatedAt               time.Time                           `json:"createdAt"`
	UpdatedAt               time.Time                           `json:"updatedAt"`
}

type PublicOSSProviderSetting struct {
	Region             string `json:"region"`
	Endpoint           string `json:"endpoint"`
	CDNBaseURL         string `json:"cdnBaseUrl"`
	Bucket             string `json:"bucket"`
	AccessKeyID        string `json:"accessKeyId"`
	HasAccessKeySecret bool   `json:"hasAccessKeySecret"`
	PublicBaseURL      string `json:"publicBaseUrl"`
	PathPrefix         string `json:"pathPrefix"`
	S3Preset           string `json:"s3Preset"`
	PathStyle          bool   `json:"pathStyle"`
	HasSessionToken    bool   `json:"hasSessionToken"`
	StorageLocationID  string `json:"storageLocationId"`
}

type ossSettingValue struct {
	Enabled           bool   `json:"enabled"`
	Provider          string `json:"provider"`
	Region            string `json:"region"`
	Endpoint          string `json:"endpoint"`
	CDNBaseURL        string `json:"cdnBaseUrl"`
	Bucket            string `json:"bucket"`
	AccessKeyID       string `json:"accessKeyId"`
	AccessKeySecret   string `json:"accessKeySecret"`
	PublicBaseURL     string `json:"publicBaseUrl"`
	PathPrefix        string `json:"pathPrefix"`
	S3Preset          string `json:"s3Preset"`
	PathStyle         bool   `json:"pathStyle"`
	SessionToken      string `json:"sessionToken"`
	StorageLocationID string `json:"storageLocationId"`
	AllowUserS3       bool   `json:"allowUserS3"`
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
	Region            string `json:"region"`
	Endpoint          string `json:"endpoint"`
	CDNBaseURL        string `json:"cdnBaseUrl"`
	Bucket            string `json:"bucket"`
	AccessKeyID       string `json:"accessKeyId"`
	AccessKeySecret   string `json:"accessKeySecret"`
	PublicBaseURL     string `json:"publicBaseUrl"`
	PathPrefix        string `json:"pathPrefix"`
	S3Preset          string `json:"s3Preset"`
	PathStyle         bool   `json:"pathStyle"`
	SessionToken      string `json:"sessionToken"`
	StorageLocationID string `json:"storageLocationId"`
}

func (s *Service) AdminOSSSetting(actor *model.User) (*PublicOSSSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	setting, value, err := s.readOSSSetting()
	if err != nil {
		return nil, err
	}
	public, err := s.publicOSSSetting(setting, value, "platform", "")
	if err != nil {
		return nil, err
	}
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
	next = archiveOSSProviderCredentials(next, currentValue)
	if next.Provider == s3Provider && next.Enabled {
		location, err := s.requireTestedS3Location("platform", "", next)
		if err != nil {
			return nil, err
		}
		next.StorageLocationID = location.ID
	} else if next.Provider == s3Provider && storageLocationDigest(next) == storageLocationDigest(currentValue) {
		next.StorageLocationID = currentValue.StorageLocationID
	}
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
	if next.Provider == s3Provider && next.StorageLocationID != "" {
		if err := s.repo.ActivateStorageLocation("platform", "", next.StorageLocationID, next.Enabled); err != nil {
			return nil, err
		}
	} else if currentValue.Provider == s3Provider && currentValue.StorageLocationID != "" {
		if err := s.repo.ActivateStorageLocation("platform", "", "", false); err != nil {
			return nil, err
		}
	}
	public, err := s.publicOSSSetting(&setting, next, "platform", "")
	if err != nil {
		return nil, err
	}
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
	_, platform, err := s.readOSSSetting()
	if err != nil {
		return nil, err
	}
	public, err := s.publicUserOSSSetting(setting, value, actor.ID, platform.AllowUserS3)
	if err != nil {
		return nil, err
	}
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
	_, platform, err := s.readOSSSetting()
	if err != nil {
		return nil, err
	}
	if next.Enabled && next.Provider == s3Provider && !platform.AllowUserS3 {
		return nil, Forbidden("平台管理员尚未允许个人 S3 兼容存储")
	}
	if next.Provider == s3Provider && next.Enabled {
		location, err := s.requireTestedS3Location("user", actor.ID, next)
		if err != nil {
			return nil, err
		}
		next.StorageLocationID = location.ID
	} else if next.Provider == s3Provider && storageLocationDigest(next) == storageLocationDigest(currentValue) {
		next.StorageLocationID = currentValue.StorageLocationID
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
	if next.Provider == s3Provider && next.StorageLocationID != "" {
		if err := s.repo.ActivateStorageLocation("user", actor.ID, next.StorageLocationID, next.Enabled); err != nil {
			return nil, err
		}
	} else if currentValue.Provider == s3Provider && currentValue.StorageLocationID != "" {
		if err := s.repo.ActivateStorageLocation("user", actor.ID, "", false); err != nil {
			return nil, err
		}
	}
	public, err := s.publicUserOSSSetting(&setting, next, actor.ID, platform.AllowUserS3)
	if err != nil {
		return nil, err
	}
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
	value.SessionToken, err = s.encryptSettingSecret(value.SessionToken)
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
		setting.SessionToken, err = s.encryptSettingSecret(setting.SessionToken)
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
	if value.SessionToken != "" && !strings.HasPrefix(value.SessionToken, encryptedSettingPrefix) {
		needsMigration = true
	}
	value.SessionToken, err = s.decryptSettingSecret(value.SessionToken)
	if err != nil {
		return false, err
	}
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
		if setting.SessionToken != "" && !strings.HasPrefix(setting.SessionToken, encryptedSettingPrefix) {
			needsMigration = true
		}
		setting.SessionToken, err = s.decryptSettingSecret(setting.SessionToken)
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
	requestHasProviderSetting := hasOSSProviderSettingRequest(req)
	next := ossSettingValue{
		Enabled:         req.Enabled,
		Provider:        strings.ToLower(strings.TrimSpace(req.Provider)),
		Region:          strings.TrimSpace(req.Region),
		Endpoint:        strings.TrimRight(strings.TrimSpace(req.Endpoint), "/"),
		CDNBaseURL:      strings.TrimRight(strings.TrimSpace(req.CDNBaseURL), "/"),
		Bucket:          strings.TrimSpace(req.Bucket),
		AccessKeyID:     strings.TrimSpace(req.AccessKeyID),
		AccessKeySecret: strings.TrimSpace(req.AccessKeySecret),
		PublicBaseURL:   strings.TrimRight(strings.TrimSpace(req.PublicBaseURL), "/"),
		PathPrefix:      strings.Trim(strings.TrimSpace(req.PathPrefix), "/"),
		S3Preset:        strings.TrimSpace(req.S3Preset),
		PathStyle:       req.PathStyle,
		SessionToken:    strings.TrimSpace(req.SessionToken),
		AllowUserS3:     req.AllowUserS3,
	}
	current = normalizeOSSSetting(current)
	if next.Provider == "" {
		next.Provider = aliyunOSSProvider
	}
	if next.Provider != current.Provider {
		next = restoreArchivedOSSProviderSetting(next, current)
	}
	next = normalizeOSSSetting(next)
	if next.Provider != aliyunOSSProvider && next.Provider != tencentCOSProvider && next.Provider != qiniuKodoProvider && next.Provider != s3Provider {
		return next, BadAuthRequest("仅支持阿里云 OSS、腾讯云 COS、七牛云 Kodo 和通用 S3")
	}
	if next.Provider == s3Provider {
		switch next.S3Preset {
		case "aws", "r2", "b2", "rustfs", "custom":
		default:
			return next, BadAuthRequest("S3 预设无效")
		}
	}
	// 同一云厂商修改存储位置时，留空仍表示保留现有密钥；切换厂商时不能复用。
	if next.AccessKeySecret == "" && next.Provider == current.Provider {
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
	if next.SessionToken == "" && next.Provider == current.Provider {
		next.SessionToken = current.SessionToken
	}
	if !next.Enabled && next.Provider == current.Provider && !requestHasProviderSetting {
		next.Region = current.Region
		next.Endpoint = current.Endpoint
		next.CDNBaseURL = current.CDNBaseURL
		next.Bucket = current.Bucket
		next.AccessKeyID = current.AccessKeyID
		next.AccessKeySecret = current.AccessKeySecret
		next.PublicBaseURL = current.PublicBaseURL
		next.PathPrefix = current.PathPrefix
		next.S3Preset = current.S3Preset
		next.PathStyle = current.PathStyle
		next.SessionToken = current.SessionToken
		next.StorageLocationID = current.StorageLocationID
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
			if next.Provider == s3Provider {
				return next, BadAuthRequest("请填写 S3 Endpoint 服务根 URL")
			}
			return next, BadAuthRequest("请填写阿里云 OSS Endpoint")
		}
		if next.Provider == s3Provider && next.Region == "" {
			return next, BadAuthRequest("请填写 S3 Region")
		}
		var endpointErr error
		if next.Provider == s3Provider {
			_, endpointErr = validateStorageEndpoint(next.Endpoint)
		} else {
			_, endpointErr = ValidateOutboundURL(next.Endpoint)
		}
		if endpointErr != nil {
			return next, endpointErr
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
	value.PublicBaseURL = strings.TrimRight(strings.TrimSpace(value.PublicBaseURL), "/")
	value.PathPrefix = strings.Trim(strings.TrimSpace(value.PathPrefix), "/")
	value.S3Preset = strings.ToLower(strings.TrimSpace(value.S3Preset))
	if value.S3Preset == "" {
		value.S3Preset = "custom"
	}
	value.SessionToken = strings.TrimSpace(value.SessionToken)
	value.StorageLocationID = strings.TrimSpace(value.StorageLocationID)
	return value
}

func ossProviderSettingFromValue(value ossSettingValue) ossProviderSetting {
	return normalizeOSSProviderSetting(ossProviderSetting{
		Region: value.Region, Endpoint: value.Endpoint, CDNBaseURL: value.CDNBaseURL, Bucket: value.Bucket,
		AccessKeyID: value.AccessKeyID, AccessKeySecret: value.AccessKeySecret, PublicBaseURL: value.PublicBaseURL,
		PathPrefix: value.PathPrefix, S3Preset: value.S3Preset, PathStyle: value.PathStyle, SessionToken: value.SessionToken,
		StorageLocationID: value.StorageLocationID,
	})
}

func ossSettingValueFromProviderSetting(provider string, value ossProviderSetting) ossSettingValue {
	value = normalizeOSSProviderSetting(value)
	return ossSettingValue{
		Provider: provider, Region: value.Region, Endpoint: value.Endpoint, CDNBaseURL: value.CDNBaseURL,
		Bucket: value.Bucket, AccessKeyID: value.AccessKeyID, AccessKeySecret: value.AccessKeySecret, PublicBaseURL: value.PublicBaseURL,
		PathPrefix: value.PathPrefix, S3Preset: value.S3Preset, PathStyle: value.PathStyle, SessionToken: value.SessionToken,
		StorageLocationID: value.StorageLocationID,
	}
}

func hasOSSProviderSetting(value ossSettingValue) bool {
	return value.Region != "" || value.Endpoint != "" || value.CDNBaseURL != "" || value.Bucket != "" ||
		value.AccessKeyID != "" || value.AccessKeySecret != "" || value.PublicBaseURL != "" || value.PathPrefix != "" ||
		value.S3Preset != "" || value.SessionToken != "" || value.StorageLocationID != ""
}

func hasOSSProviderSettingRequest(req OSSSettingRequest) bool {
	return strings.TrimSpace(req.Region) != "" || strings.TrimSpace(req.Endpoint) != "" ||
		strings.TrimSpace(req.CDNBaseURL) != "" || strings.TrimSpace(req.Bucket) != "" ||
		strings.TrimSpace(req.AccessKeyID) != "" || strings.TrimSpace(req.AccessKeySecret) != "" ||
		strings.TrimSpace(req.PublicBaseURL) != "" || strings.TrimSpace(req.PathPrefix) != "" ||
		strings.TrimSpace(req.S3Preset) != "" || req.PathStyle || strings.TrimSpace(req.SessionToken) != ""
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
	next.AccessKeySecret = firstNonEmpty(next.AccessKeySecret, archived.AccessKeySecret)
	next.PublicBaseURL = firstNonEmpty(next.PublicBaseURL, archived.PublicBaseURL)
	next.S3Preset = firstNonEmpty(next.S3Preset, archived.S3Preset)
	if !next.PathStyle {
		next.PathStyle = archived.PathStyle
	}
	next.SessionToken = firstNonEmpty(next.SessionToken, archived.SessionToken)
	next.StorageLocationID = firstNonEmpty(next.StorageLocationID, archived.StorageLocationID)
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
	if value.PathPrefix == "" {
		value.PathPrefix = defaultOSSPathPrefix
	}
	value.S3Preset = strings.ToLower(strings.TrimSpace(value.S3Preset))
	if value.S3Preset == "" {
		value.S3Preset = "custom"
	}
	value.SessionToken = strings.TrimSpace(value.SessionToken)
	value.StorageLocationID = strings.TrimSpace(value.StorageLocationID)
	value.ArchivedCredentials = cloneOSSProviderCredentials(value.ArchivedCredentials)
	value.ArchivedSettings = cloneOSSProviderSettings(value.ArchivedSettings)
	return value
}

func defaultOSSSetting() ossSettingValue {
	return ossSettingValue{Provider: aliyunOSSProvider, PathPrefix: defaultOSSPathPrefix}
}

func (s *Service) publicOSSSetting(setting *model.SystemSetting, value ossSettingValue, scope string, ownerID string) (PublicOSSSetting, error) {
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
		S3Preset:           value.S3Preset,
		PathStyle:          value.PathStyle,
		HasSessionToken:    value.SessionToken != "",
		StorageLocationID:  value.StorageLocationID,
		AllowUserS3:        value.AllowUserS3,
		ProviderSettings:   publicOSSProviderSettings(value),
	}
	if setting != nil {
		result.UpdatedBy = setting.UpdatedBy
		result.CreatedAt = setting.CreatedAt
		result.UpdatedAt = setting.UpdatedAt
	}
	if err := s.populateStorageLocationPublic(&result, scope, ownerID, value.StorageLocationID); err != nil {
		return PublicOSSSetting{}, err
	}
	return result, nil
}

func (s *Service) publicUserOSSSetting(setting *model.UserOSSSetting, value ossSettingValue, ownerID string, allowUserS3 bool) (PublicOSSSetting, error) {
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
		S3Preset:           value.S3Preset,
		PathStyle:          value.PathStyle,
		HasSessionToken:    value.SessionToken != "",
		StorageLocationID:  value.StorageLocationID,
		AllowUserS3:        allowUserS3,
		ProviderSettings:   publicOSSProviderSettings(value),
	}
	if value.Provider == s3Provider && !allowUserS3 {
		result.Enabled = false
	}
	if setting != nil {
		result.UpdatedBy = setting.UserID
		result.CreatedAt = setting.CreatedAt
		result.UpdatedAt = setting.UpdatedAt
	}
	if err := s.populateStorageLocationPublic(&result, "user", ownerID, value.StorageLocationID); err != nil {
		return PublicOSSSetting{}, err
	}
	return result, nil
}

func publicOSSProviderSettings(value ossSettingValue) map[string]PublicOSSProviderSetting {
	result := make(map[string]PublicOSSProviderSetting, len(value.ArchivedSettings)+1)
	for provider, setting := range value.ArchivedSettings {
		result[provider] = publicOSSProviderSettingFromSetting(setting)
	}
	if value.Provider != "" {
		result[value.Provider] = publicOSSProviderSettingFromValue(value)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func publicOSSProviderSettingFromSetting(value ossProviderSetting) PublicOSSProviderSetting {
	return PublicOSSProviderSetting{
		Region: value.Region, Endpoint: value.Endpoint, CDNBaseURL: value.CDNBaseURL, Bucket: value.Bucket,
		AccessKeyID: value.AccessKeyID, HasAccessKeySecret: value.AccessKeySecret != "", PublicBaseURL: value.PublicBaseURL,
		PathPrefix: value.PathPrefix, S3Preset: value.S3Preset, PathStyle: value.PathStyle,
		HasSessionToken: value.SessionToken != "", StorageLocationID: value.StorageLocationID,
	}
}

func publicOSSProviderSettingFromValue(value ossSettingValue) PublicOSSProviderSetting {
	return PublicOSSProviderSetting{
		Region: value.Region, Endpoint: value.Endpoint, CDNBaseURL: value.CDNBaseURL, Bucket: value.Bucket,
		AccessKeyID: value.AccessKeyID, HasAccessKeySecret: value.AccessKeySecret != "", PublicBaseURL: value.PublicBaseURL,
		PathPrefix: value.PathPrefix, S3Preset: value.S3Preset, PathStyle: value.PathStyle,
		HasSessionToken: value.SessionToken != "", StorageLocationID: value.StorageLocationID,
	}
}

func storageLocationDigest(value ossSettingValue) string {
	value = normalizeOSSSetting(value)
	payload := strings.Join([]string{value.Provider, value.Endpoint, value.Bucket, value.Region, strconv.FormatBool(value.PathStyle), value.PathPrefix}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func storageTestDigest(value ossSettingValue) string {
	payload := strings.Join([]string{storageLocationDigest(value), value.AccessKeyID, value.AccessKeySecret, value.SessionToken}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}
