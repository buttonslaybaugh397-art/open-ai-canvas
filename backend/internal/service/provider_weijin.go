package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"infinite-canvas/backend/internal/protocol"
)

const weijinUploadLimit int64 = 50 << 20

func prepareWeijinGenerationMedia(ctx context.Context, input canvasGenerationInput) (canvasGenerationInput, error) {
	uploaded := make(map[string]string)
	groups := []*[]providerMedia{&input.ReferenceImages, &input.ReferenceVideos, &input.ReferenceAudios}
	for _, group := range groups {
		for index := range *group {
			media := &(*group)[index]
			cacheKey := weijinMediaCacheKey(*media)
			if uploadedURL := uploaded[cacheKey]; uploadedURL != "" {
				media.URL = uploadedURL
				media.DataURL = ""
				continue
			}
			uploadedURL, err := uploadWeijinMedia(ctx, input.Config, *media)
			if err != nil {
				return input, err
			}
			if cacheKey != "" {
				uploaded[cacheKey] = uploadedURL
			}
			media.URL = uploadedURL
			media.DataURL = ""
		}
	}
	return input, nil
}

func weijinMediaCacheKey(media providerMedia) string {
	if value := strings.TrimSpace(media.URL); value != "" {
		return "url:" + value
	}
	if value := strings.TrimSpace(media.DataURL); value != "" {
		return "data:" + value
	}
	return ""
}

func uploadWeijinMedia(ctx context.Context, config providerConfig, media providerMedia) (string, error) {
	raw, mimeType, err := weijinMediaBytes(ctx, media)
	if err != nil {
		return "", fmt.Errorf("读取微进参考素材失败：%w", err)
	}
	if len(raw) == 0 {
		return "", errors.New("微进参考素材为空")
	}
	if int64(len(raw)) > weijinUploadLimit {
		return "", errors.New("微进参考素材超过上传接口 50MB 限制")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name":     "file",
		"filename": providerMediaFilename(media, mimeType),
	}))
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", fmt.Errorf("创建微进素材上传请求失败：%w", err)
	}
	if _, err := part.Write(raw); err != nil {
		return "", fmt.Errorf("写入微进素材上传请求失败：%w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("完成微进素材上传请求失败：%w", err)
	}

	uploadURL, err := protocolRequestURL(config.BaseURL, protocolUploadRequestSpec())
	if err != nil {
		return "", fmt.Errorf("微进素材上传地址无效：%w", err)
	}
	req, err := http.NewRequestWithContext(withProviderRequestKind(ctx, "upload"), http.MethodPost, uploadURL, body)
	if err != nil {
		return "", err
	}
	applyProviderAuth(req, config)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ApplyOutboundHeaders(req, config.Headers)
	data, _, err := doBinary(req)
	if err != nil {
		return "", fmt.Errorf("微进参考素材上传失败：%w", err)
	}
	var response struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return "", errors.New("微进素材上传响应不是有效 JSON")
	}
	resultURL := strings.TrimSpace(response.URL)
	parsed, err := url.Parse(resultURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
		return "", errors.New("微进素材上传响应缺少有效的 HTTPS 地址")
	}
	return resultURL, nil
}

func protocolUploadRequestSpec() protocol.RequestSpec {
	return protocol.RequestSpec{Method: http.MethodPost, Path: "/api/upload/video", OriginPath: true}
}

func weijinMediaBytes(ctx context.Context, media providerMedia) ([]byte, string, error) {
	if strings.HasPrefix(strings.TrimSpace(media.DataURL), "data:") {
		return mediaBytes(media)
	}
	mediaURL := strings.TrimSpace(media.URL)
	if mediaURL == "" {
		return nil, "", errors.New("参考素材缺少可读取地址")
	}
	data, mimeType, err := getExternalBinary(withProviderRequestKind(ctx, "upload-source"), mediaURL)
	if err != nil {
		return nil, "", err
	}
	return data, normalizedMediaMimeType(firstNonEmpty(media.MimeType, media.Type, mimeType), data), nil
}
