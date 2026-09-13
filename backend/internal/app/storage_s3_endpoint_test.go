package app

import (
	"net/http/httptest"
	"testing"
)

func TestS3ExpectContinueDefaultsDependOnEndpoint(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	server := httptest.NewServer(nil)
	defer server.Close()
	custom, err := newS3Client(ossSettingValue{
		Provider:        s3Provider,
		Region:          "us-east-1",
		Endpoint:        server.URL,
		Bucket:          "bucket",
		AccessKeyID:     "id",
		AccessKeySecret: "secret",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if custom.Config.S3Disable100Continue == nil || !*custom.Config.S3Disable100Continue {
		t.Fatal("custom S3 endpoint should disable Expect: 100-Continue")
	}
	if standardAWSS3Endpoint("https://s3.amazonaws.com") {
		return
	}
	t.Fatal("AWS S3 endpoint was not recognized")
}
