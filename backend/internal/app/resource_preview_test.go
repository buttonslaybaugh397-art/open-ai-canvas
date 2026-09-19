package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestCDNProbeDoesNotFollowRedirectToOrigin(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	var originReads atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originReads.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, origin.URL, http.StatusTemporaryRedirect)
	}))
	defer cdn.Close()
	available, err := probeResourceURL(context.Background(), cdn.URL)
	if err == nil || available || originReads.Load() != 0 {
		t.Fatal("CDN redirects must fail without probing the origin")
	}
}

func TestS3PreviewUsesConfiguredCDNWithoutAttachment(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Error("preparing preview must not relay media")
		}
		if r.URL.Path == "/missing.mp4" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer cdn.Close()
	svc := newResourceTestService(t)
	setting := ossSettingValue{Enabled: true, Provider: s3Provider, Endpoint: "https://storage.example.com", CDNBaseURL: cdn.URL, Region: "us-east-1", Bucket: "bucket", AccessKeyID: "id", AccessKeySecret: "secret"}
	saveSetting := func() {
		t.Helper()
		value, err := json.Marshal(setting)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(value)}); err != nil {
			t.Fatal(err)
		}
	}
	saveSetting()
	resource := model.Resource{ID: "video", UserID: "owner", Kind: "video", Status: model.ResourceStatusReady, Provider: s3Provider, Endpoint: setting.Endpoint, Bucket: setting.Bucket, ObjectKey: "video.mp4"}
	if err := svc.repo.CreateResource(&resource); err != nil {
		t.Fatal(err)
	}
	for _, options := range []ResourceDeliveryOptions{{}, {ForceDirect: true, DownloadFileName: "final.mp4"}, {}} {
		delivery, err := svc.PrepareResourceDelivery("owner", resource.ID, options)
		if err != nil {
			t.Fatal(err)
		}
		target, err := url.Parse(delivery.RedirectURL)
		if err != nil || target.Scheme+"://"+target.Host != cdn.URL || target.Path != "/video.mp4" {
			t.Fatalf("configured CDN not used: %#v", delivery)
		}
		if options.DownloadFileName == "" && target.RawQuery != "" {
			t.Fatal("preview inherited attachment parameters")
		}
	}
	proxy, err := svc.PrepareResourceDelivery("owner", resource.ID, ResourceDeliveryOptions{ForceProxy: true})
	if err != nil || !strings.HasPrefix(proxy.RedirectURL, cdn.URL) {
		t.Fatal("legacy proxy flag must not bypass CDN")
	}
	resource.ID = "missing"
	resource.ObjectKey = "missing.mp4"
	if err := svc.repo.CreateResource(&resource); err != nil {
		t.Fatal(err)
	}
	missing, err := svc.PrepareResourceDelivery("owner", resource.ID, ResourceDeliveryOptions{})
	if err == nil || missing != nil {
		t.Fatal("missing CDN object must return an error without origin delivery")
	}
	setting.CDNBaseURL = ""
	saveSetting()
	noCDN, err := svc.PrepareResourceDelivery("owner", "video", ResourceDeliveryOptions{})
	if err == nil || noCDN != nil {
		t.Fatal("S3 without CDN must not proxy the origin")
	}
}
