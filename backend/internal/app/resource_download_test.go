package app

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestSignedResourceDownloadsIncludeAttachmentInSignature(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	origin := httptest.NewServer(http.NotFoundHandler())
	defer origin.Close()
	name := "\u7ec8\u7a3f video #1.mp4"
	key := "users/owner/video/clip.mp4"
	for _, provider := range []string{aliyunOSSProvider, tencentCOSProvider, qiniuKodoProvider, s3Provider} {
		t.Run(provider, func(t *testing.T) {
			setting := ossSettingValue{Provider: provider, Endpoint: "https://oss-cn-test.aliyuncs.com", Bucket: "test-bucket", AccessKeyID: "test-access", AccessKeySecret: "test-secret"}
			switch provider {
			case tencentCOSProvider:
				setting.Endpoint = "https://cos.ap-guangzhou.myqcloud.com"
				setting.Region = "ap-guangzhou"
			case qiniuKodoProvider:
				setting.Region = "z0"
			case s3Provider:
				setting.Endpoint = origin.URL
				setting.Region = "us-east-1"
			}
			expires := time.Now().Add(time.Minute)
			raw, err := signedOSSObjectURL(setting, key, expires, name)
			if err != nil {
				t.Fatal(err)
			}
			target, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			query := target.Query()
			disposition := query.Get("response-content-disposition")
			mediaType, params, err := mime.ParseMediaType(disposition)
			if err != nil || mediaType != "attachment" || params["filename"] != name {
				t.Fatalf("attachment missing or filename incorrect: %q", disposition)
			}
			switch provider {
			case aliyunOSSProvider:
				canonical := "/" + setting.Bucket + "/" + key + "?response-content-disposition=" + disposition
				mac := hmac.New(sha1.New, []byte(setting.AccessKeySecret))
				_, _ = mac.Write([]byte(strings.Join([]string{http.MethodGet, "", "", query.Get("Expires"), canonical}, "\n")))
				if query.Get("Signature") != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
					t.Fatal("attachment must be part of the OSS signature")
				}
			case tencentCOSProvider:
				if !strings.Contains(query.Get("q-url-param-list"), "response-content-disposition") {
					t.Fatal("attachment must be part of the COS signature")
				}
			default:
				if query.Get("X-Amz-Signature") == "" {
					t.Fatal("S3 signature missing")
				}
			}
			preview, err := signedOSSObjectURL(setting, key, expires)
			if err != nil {
				t.Fatal(err)
			}
			previewURL, _ := url.Parse(preview)
			if previewURL.Query().Get("response-content-disposition") != "" {
				t.Fatal("preview must not request an attachment")
			}
		})
	}
}

func TestPrepareResourceDownloadKeepsCDNAndAttachment(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Error("download preparation must not relay media")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cdn.Close()
	for _, provider := range []string{aliyunOSSProvider, tencentCOSProvider, s3Provider} {
		t.Run(provider, func(t *testing.T) {
			svc := newResourceTestService(t)
			setting, _ := json.Marshal(ossSettingValue{Enabled: true, Provider: provider, Endpoint: "https://storage.example.com", CDNBaseURL: cdn.URL, Region: "us-east-1", Bucket: "bucket", AccessKeyID: "id", AccessKeySecret: "secret"})
			if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(setting)}); err != nil {
				t.Fatal(err)
			}
			resource := model.Resource{ID: "video", UserID: "owner", Kind: "video", Status: model.ResourceStatusReady, Provider: provider, Endpoint: "https://storage.example.com", Bucket: "bucket", ObjectKey: "video.mp4"}
			if err := svc.repo.CreateResource(&resource); err != nil {
				t.Fatal(err)
			}
			delivery, err := svc.PrepareResourceDelivery("owner", resource.ID, ResourceDeliveryOptions{ForceDirect: true, DownloadFileName: "final.mp4"})
			if err != nil {
				t.Fatal(err)
			}
			target, err := url.Parse(delivery.RedirectURL)
			if err != nil || !strings.HasPrefix(delivery.RedirectURL, cdn.URL+"/") || target.Query().Get("response-content-disposition") != "attachment; filename=final.mp4" {
				t.Fatal("CDN download must retain its attachment override")
			}
		})
	}
}
