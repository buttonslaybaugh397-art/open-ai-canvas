package app

import "testing"

func TestNormalizeOSSSettingRainyunPresetLocksEndpointRegionAndPathStyle(t *testing.T) {
	value := normalizeOSSSetting(ossSettingValue{
		Provider: s3Provider,
		S3Preset: rainyunS3Preset,
		Region:   "wrong-region",
		Endpoint: "https://example.invalid",
	})
	if value.Region != rainyunS3Region || value.Endpoint != rainyunS3Endpoint || !value.PathStyle {
		t.Fatalf("Rainyun preset normalization = %+v", value)
	}
}
