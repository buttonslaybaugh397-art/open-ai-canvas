package main

import (
	"sync"
	"testing"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestMigrationListCoversSchemaModels(t *testing.T) {
	want := make(map[string]bool, len(database.Models()))
	cache := &sync.Map{}
	for _, value := range database.Models() {
		parsed, err := schema.Parse(value, cache, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		want[parsed.Table] = false
	}
	for _, migration := range migrations() {
		if _, exists := want[migration.name]; !exists {
			t.Fatalf("migration list contains unknown table %q", migration.name)
		}
		if want[migration.name] {
			t.Fatalf("migration list contains duplicate table %q", migration.name)
		}
		want[migration.name] = true
	}
	for table, covered := range want {
		if !covered {
			t.Errorf("migration list is missing table %q", table)
		}
	}
}

func TestPrimaryKeyOrderSupportsCompositeKeys(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:migration-primary-key-order?mode=memory&cache=shared"))
	if err != nil {
		t.Fatal(err)
	}
	order, err := primaryKeyOrder[model.TeamMember](db)
	if err != nil {
		t.Fatal(err)
	}
	if order != "team_id, user_id" {
		t.Fatalf("unexpected composite primary key order: %q", order)
	}
}
