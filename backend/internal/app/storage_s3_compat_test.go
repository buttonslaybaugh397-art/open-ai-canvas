package app

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestS3CompatibleUploadDisablesExpectContinue(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	const size = 2*1024*1024 + 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Expect"); got != "" {
			t.Errorf("Expect header = %q, want empty for custom S3 endpoint", got)
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if len(data) != size {
			t.Errorf("uploaded bytes = %d, want %d", len(data), size)
		}
		w.Header().Set("ETag", `"compat-etag"`)
	}))
	defer server.Close()

	setting := ossSettingValue{Provider: s3Provider, Region: "us-east-1", Endpoint: server.URL, Bucket: "bucket", AccessKeyID: "test-id", AccessKeySecret: "test-secret"}
	etag, err := putS3Object(setting, "large.bin", "application/octet-stream", size, bytes.NewReader(bytes.Repeat([]byte{'x'}, size)))
	if err != nil || etag != "compat-etag" {
		t.Fatalf("putS3Object() = %q, %v", etag, err)
	}
}
