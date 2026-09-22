package app

import (
	"strings"
	"testing"
)

func TestValidateCanonicalAgentGeminiBodyRejectsEmptyFileData(t *testing.T) {
	body := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{"text": "分析这些素材"},
					map[string]interface{}{"fileData": map[string]interface{}{"fileUri": "", "mimeType": ""}},
				},
			},
		},
	}

	err := validateCanonicalAgentGeminiBody(body)
	if err == nil || !strings.Contains(err.Error(), "图片资源地址为空") || !strings.Contains(err.Error(), "内容 [0].parts[1]") {
		t.Fatalf("error = %v, want indexed empty media error", err)
	}
}

func TestValidateCanonicalAgentGeminiBodyAcceptsValidMediaParts(t *testing.T) {
	body := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{"text": "分析图片"},
					map[string]interface{}{"inlineData": map[string]interface{}{"mimeType": "image/png", "data": "aGVsbG8="}},
					map[string]interface{}{"fileData": map[string]interface{}{"fileUri": "resource:image-1", "mimeType": "image/png"}},
				},
			},
		},
	}

	if err := validateCanonicalAgentGeminiBody(body); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAgentGeminiPayloadFindsEmptyNestedContents(t *testing.T) {
	body := map[string]interface{}{
		"request": map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{"text": "分析"},
						map[string]interface{}{"fileData": map[string]interface{}{"fileUri": "", "mimeType": ""}},
					},
				},
			},
		},
	}

	err := validateAgentGeminiPayload(body)
	if err == nil || !strings.Contains(err.Error(), "contents[0].parts[1]") || !strings.Contains(err.Error(), "图片资源地址为空") {
		t.Fatalf("error = %v, want nested empty media error", err)
	}
}
