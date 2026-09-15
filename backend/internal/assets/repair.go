package assets

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
)

// RebindDocumentResource only changes complete locators in persisted media fields.
// UseNumber keeps unrelated large integers and decimal values lossless.
func RebindDocumentResource(raw, oldID, replacementID string) (string, bool, error) {
	if oldID == "" || replacementID == "" || ValidID(oldID) != oldID || ValidID(replacementID) != replacementID || oldID == replacementID {
		return "", false, errors.New("invalid resource repair IDs")
	}
	if strings.TrimSpace(raw) == "" {
		return raw, false, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return "", false, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", false, errors.New("invalid resource reference document")
	}
	document, changed := rebindDocumentResource(document, "", oldID, replacementID)
	if !changed {
		return raw, false, nil
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return "", false, err
	}
	return string(encoded), true, nil
}

func rebindDocumentResource(value any, field, oldID, replacementID string) (any, bool) {
	changed := false
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			next, updated := rebindDocumentResource(child, key, oldID, replacementID)
			if updated {
				item[key] = next
				changed = true
			}
		}
	case []any:
		for index, child := range item {
			next, updated := rebindDocumentResource(child, field, oldID, replacementID)
			if updated {
				item[index] = next
				changed = true
			}
		}
	case string:
		switch field {
		case "storageKey", "resourceKey", "content", "dataUrl", "url", "coverUrl":
			next := rebindResourceLocator(item, oldID, replacementID)
			return next, next != item
		}
	}
	return value, changed
}

func rebindResourceLocator(value, oldID, replacementID string) string {
	if value == "resource:"+oldID {
		return "resource:" + replacementID
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || strings.ContainsAny(value, " \t\r\n") {
		return value
	}
	if parsed.IsAbs() {
		if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return value
		}
	} else if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return value
	}
	for _, prefix := range []string{"/api/resources/", "/api/public/resources/"} {
		oldPath := prefix + oldID + "/file"
		path := parsed.EscapedPath()
		if path != oldPath {
			// Public downloads may include one cosmetic filename segment.
			if prefix != "/api/public/resources/" || !strings.HasPrefix(path, oldPath+"/") {
				continue
			}
			filename := strings.TrimPrefix(path, oldPath+"/")
			if filename == "" || strings.Contains(filename, "/") {
				continue
			}
		}
		// Keep query ordering, escaping and fragments byte-for-byte unchanged.
		start := strings.Index(value, path)
		if start >= 0 {
			return value[:start] + prefix + replacementID + "/file" + value[start+len(oldPath):]
		}
	}
	return value
}
