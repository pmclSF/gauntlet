package world

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestSeedDBAndCleanup(t *testing.T) {
	runDir := t.TempDir()
	dbDef := &DBDef{
		Database: "orders",
		SeedSets: map[string]*SeedSetDef{
			"nominal": {
				Tables: map[string]*TableDef{
					"orders": {
						Columns: map[string]string{
							"status": "TEXT",
							"id":     "INTEGER",
							"meta":   "TEXT",
						},
						Rows: []map[string]interface{}{
							{
								"id":     1,
								"status": "open",
								"meta": map[string]interface{}{
									"region": "us",
									"tags":   []interface{}{"vip", "priority"},
								},
							},
						},
					},
				},
			},
		},
	}

	dbPath, err := SeedDB(dbDef, []string{"nominal"}, runDir)
	if err != nil {
		t.Fatalf("SeedDB: %v", err)
	}
	if !strings.HasSuffix(dbPath, string(filepath.Separator)+"orders.db") {
		t.Fatalf("db path = %q, want suffix %q", dbPath, filepath.Join("", "orders.db"))
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()

	var (
		id     int
		status string
		meta   string
	)
	if err := db.QueryRow(`SELECT id, status, meta FROM orders`).Scan(&id, &status, &meta); err != nil {
		t.Fatalf("query seeded row: %v", err)
	}
	if id != 1 || status != "open" {
		t.Fatalf("seeded row = id:%d status:%q, want id:1 status:\"open\"", id, status)
	}

	var parsedMeta map[string]interface{}
	if err := json.Unmarshal([]byte(meta), &parsedMeta); err != nil {
		t.Fatalf("meta json decode: %v", err)
	}
	if parsedMeta["region"] != "us" {
		t.Fatalf("meta.region = %v, want us", parsedMeta["region"])
	}

	if err := CleanupDB(dbPath); err != nil {
		t.Fatalf("CleanupDB: %v", err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("expected db to be removed, err=%v", err)
	}
}

func TestSeedDBMissingSeedSetFails(t *testing.T) {
	runDir := t.TempDir()
	dbDef := &DBDef{
		Database: "orders",
		SeedSets: map[string]*SeedSetDef{
			"nominal": {Tables: map[string]*TableDef{}},
		},
	}

	_, err := SeedDB(dbDef, []string{"missing"}, runDir)
	if err == nil {
		t.Fatal("expected error for missing seed set")
	}
	if !strings.Contains(err.Error(), "seed set 'missing' not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSeedDBWithEmptyTablesIsNoOp(t *testing.T) {
	runDir := t.TempDir()
	dbDef := &DBDef{
		Database: "empty",
		SeedSets: map[string]*SeedSetDef{
			"empty": {Tables: nil},
		},
	}

	dbPath, err := SeedDB(dbDef, []string{"empty"}, runDir)
	if err != nil {
		t.Fatalf("SeedDB: %v", err)
	}
	if got := filepath.Base(dbPath); got != "empty.db" {
		t.Fatalf("db filename = %q, want empty.db", got)
	}
}
