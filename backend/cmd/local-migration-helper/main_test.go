package main

import "testing"

func TestSafeArchiveName(t *testing.T) {
	tests := []struct {
		name string
		safe bool
	}{
		{name: "manifest.json", safe: true},
		{name: "data/project-workbench-debug/", safe: true},
		{name: "../outside", safe: false},
		{name: "../", safe: false},
		{name: "/absolute", safe: false},
		{name: "service-config\\\\.env", safe: false},
		{name: "data/./file", safe: false},
	}
	for _, test := range tests {
		if got := safeArchiveName(test.name); got != test.safe {
			t.Errorf("safeArchiveName(%q) = %t, want %t", test.name, got, test.safe)
		}
	}
}
