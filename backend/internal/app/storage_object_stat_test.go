package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStorageObjectStatProvidersUseAuthenticatedMetadata(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	for _, provider := range []string{s3Provider, aliyunOSSProvider, tencentCOSProvider} {
		for _, status := range []int{200, 403, 404, 503} {
			t.Run(provider+http.StatusText(status), func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.Method != http.MethodHead || r.Header.Get("Authorization") == "" || !strings.HasSuffix(r.URL.Path, "/folder/object") {
						t.Errorf("bad stat request: %s %s", r.Method, r.URL.Path)
					}
					w.Header().Set("Content-Length", "1234")
					w.WriteHeader(status)
				}))
				defer server.Close()
				bucket := "bucket"
				if provider == aliyunOSSProvider {
					// ossBucketBaseURL uses virtual hosts. The loopback host already
					// starts with this synthetic bucket, avoiding a DNS dependency.
					bucket = "127"
				}
				stat, err := statStoredCloudObject(context.Background(), ossSettingValue{
					Provider: provider, Endpoint: server.URL, Bucket: bucket, Region: "us-east-1",
					AccessKeyID: "test-id", AccessKeySecret: "test-secret", CDNBaseURL: "https://cdn.invalid",
				}, "folder/object")
				if (status == 200 || status == 404) != (err == nil) {
					t.Fatalf("status=%d stat=%+v error=%v", status, stat, err)
				}
				if status == 200 && (!stat.Exists || stat.Bytes != 1234 || stat.CheckedAt.IsZero()) {
					t.Fatalf("size not read from metadata: %+v", stat)
				}
				if status == 404 && (stat.Exists || stat.Bytes != 0) {
					t.Fatalf("missing object counted: %+v", stat)
				}
				if calls.Load() != 1 {
					t.Fatalf("metadata query was retried %d times", calls.Load())
				}
			})
		}
	}
}

func TestStorageStatRejectsUnknownLengthAndQiniuFailures(t *testing.T) {
	if _, err := storageStatHTTPResult(200, -1); err == nil {
		t.Fatal("unknown content length accepted")
	}
	for _, item := range []struct {
		status int
		body   string
		ok     bool
		size   int64
	}{
		{200, `{"fsize":123}`, true, 123},
		{200, `{"fsize":0}`, true, 0},
		{612, `{}`, true, 0},
		{404, `{}`, false, 0},
		{403, `{}`, false, 0},
		{503, `{}`, false, 0},
		{200, `{}`, false, 0},
		{200, `{"fsize":-1}`, false, 0},
		{200, `{"fsize":0.5}`, false, 0},
		{200, `{"fsize":9223372036854775808}`, false, 0},
		{200, `invalid`, false, 0},
	} {
		stat, err := decodeQiniuStorageStat(item.status, strings.NewReader(item.body))
		if (err == nil) != item.ok || (item.ok && stat.Bytes != item.size) {
			t.Fatalf("%+v => %+v %v", item, stat, err)
		}
	}
}
