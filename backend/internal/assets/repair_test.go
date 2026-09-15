package assets

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRebindDocumentResourceExactLocators(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
	}{
		{"resource:old", "resource:new"},
		{"/api/resources/old/file", "/api/resources/new/file"},
		{"/api/resources/old/file?download=1&x=%2f#t=0.5", "/api/resources/new/file?download=1&x=%2f#t=0.5"},
		{"https://host.example/api/resources/old/file?x=1&x=2#part", "https://host.example/api/resources/new/file?x=1&x=2#part"},
		{"/api/public/resources/old/file/old.png?signature=a%2Bb#part", "/api/public/resources/new/file/old.png?signature=a%2Bb#part"},
		{"resource:older", "resource:older"},
		{"resource:old?x=1", "resource:old?x=1"},
		{"mention resource:old", "mention resource:old"},
		{"see https://host.example/api/resources/old/file", "see https://host.example/api/resources/old/file"},
		{"https://host.example/?next=/api/resources/old/file", "https://host.example/?next=/api/resources/old/file"},
		{"/api/resources/old/file/more", "/api/resources/old/file/more"},
		{"/api/resources/old/other", "/api/resources/old/other"},
		{"/api/resources/older/file", "/api/resources/older/file"},
		{"/prefix/api/resources/old/file", "/prefix/api/resources/old/file"},
		{"data:text/plain,/api/resources/old/file", "data:text/plain,/api/resources/old/file"},
		{"//host.example/api/resources/old/file", "//host.example/api/resources/old/file"},
		{"https://name:pass@host.example/api/resources/old/file", "https://name:pass@host.example/api/resources/old/file"},
	} {
		t.Run(test.value, func(t *testing.T) {
			if got := rebindResourceLocator(test.value, "old", "new"); got != test.want {
				t.Fatalf("locator = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRebindDocumentResourcePreservesUnrelatedJSON(t *testing.T) {
	const raw = `{
		"updatedAt":"2026-01-01T00:00:00Z",
		"large":9007199254740993,"decimal":1.2300,
		"title":"resource:old","prompt":"/api/resources/old/file",
		"resourceId":"old","unknown":"resource:old",
		"content":"Mention resource:old and /api/resources/old/file in dialogue",
		"nested":[{"storageKey":"resource:old","resourceKey":"resource:old",
			"content":"resource:old","dataUrl":"/api/resources/old/file",
			"url":"/api/resources/old/file?x=%2f#part","coverUrl":"resource:old"}],
		"url":["resource:old",{"caption":"resource:old"}]
	}`
	next, changed, err := RebindDocumentResource(raw, "old", "new")
	if err != nil || !changed {
		t.Fatalf("repair = %t, %v", changed, err)
	}
	for _, preserved := range []string{
		`"large":9007199254740993`, `"decimal":1.2300`,
		`"title":"resource:old"`, `"prompt":"/api/resources/old/file"`,
		`"resourceId":"old"`, `"unknown":"resource:old"`,
		`"caption":"resource:old"`, `"updatedAt":"2026-01-01T00:00:00Z"`,
		`"content":"Mention resource:old and /api/resources/old/file in dialogue"`,
	} {
		if !strings.Contains(next, preserved) {
			t.Fatalf("lost unrelated content %s in %s", preserved, next)
		}
	}
	var value struct {
		Nested []map[string]string `json:"nested"`
	}
	if err := json.Unmarshal([]byte(next), &value); err != nil {
		t.Fatal(err)
	}
	for key, locator := range value.Nested[0] {
		if strings.Contains(locator, "old") {
			t.Fatalf("%s was not repaired: %s", key, locator)
		}
	}
	repeated, changed, err := RebindDocumentResource(next, "old", "new")
	if err != nil || changed || repeated != next {
		t.Fatalf("repeat = %q, %t, %v", repeated, changed, err)
	}
}

func TestRebindDocumentResourceNoopAndInvalidJSON(t *testing.T) {
	for _, raw := range []string{` { "prompt" : "resource:old" } `, "", `{"content":"unrelated"}`} {
		next, changed, err := RebindDocumentResource(raw, "old", "new")
		if err != nil || changed || next != raw {
			t.Fatalf("no-op(%s) = %q, %t, %v", raw, next, changed, err)
		}
	}
	for _, raw := range []string{`{"storageKey":`, `{"storageKey":"resource:old"} {}`, `{"storageKey":"resource:old"} trailing`} {
		if _, _, err := RebindDocumentResource(raw, "old", "new"); err == nil {
			t.Fatalf("accepted invalid JSON: %s", raw)
		}
	}
}
