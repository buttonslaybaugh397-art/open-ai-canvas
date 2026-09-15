package assets

import "testing"

func TestDocumentReferencedIDsIncludesMetadataResourceKey(t *testing.T) {
	resourceIDs := map[string]struct{}{"resource-metadata-key": {}}
	got := DocumentReferencedIDs("{\"metadata\":{\"resourceKey\":\"resource:resource-metadata-key\"}}", resourceIDs)
	if _, ok := got["resource-metadata-key"]; !ok {
		t.Fatalf("resourceKey reference was not collected: %#v", got)
	}
}
