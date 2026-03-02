package world

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// SeedDB creates an ephemeral SQLite database seeded with the given seed sets.
// Returns the path to the database file. Caller is responsible for cleanup.
func SeedDB(dbDef *DBDef, seedSetNames []string, runDir string) (string, error) {
	dbPath := filepath.Join(runDir, dbDef.Database+".db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return "", fmt.Errorf("failed to create ephemeral DB %s: %w", dbDef.Database, err)
	}
	defer db.Close()

	for _, seedName := range seedSetNames {
		seedSet, ok := dbDef.SeedSets[seedName]
		if !ok {
			return "", fmt.Errorf("seed set '%s' not found in DB definition '%s'", seedName, dbDef.Database)
		}

		if err := applySeedSet(db, seedSet); err != nil {
			return "", fmt.Errorf("failed to apply seed set '%s': %w", seedName, err)
		}
	}

	return dbPath, nil
}

func applySeedSet(db *sql.DB, seed *SeedSetDef) error {
	if seed.Tables == nil {
		return nil
	}

	for tableName, tableDef := range seed.Tables {
		if err := seedTableDef(db, tableName, tableDef); err != nil {
			return fmt.Errorf("table %s: %w", tableName, err)
		}
	}

	return nil
}

func seedTableDef(db *sql.DB, table string, td *TableDef) error {
	if td == nil || len(td.Rows) == 0 {
		return nil
	}

	// Build column list from the schema definition, or infer from rows
	var cols []string
	var colDefs []string
	if len(td.Columns) > 0 {
		for name, colType := range td.Columns {
			cols = append(cols, name)
			colDefs = append(colDefs, name+" "+colType)
		}
	} else {
		// Infer columns from first row
		for k := range td.Rows[0] {
			cols = append(cols, k)
			colDefs = append(colDefs, k+" TEXT")
		}
	}

	// Create table
	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", table, strings.Join(colDefs, ", "))
	if _, err := db.Exec(createSQL); err != nil {
		return fmt.Errorf("CREATE TABLE %s: %w", table, err)
	}

	// Insert rows
	placeholders := strings.Repeat("?,", len(cols))
	placeholders = placeholders[:len(placeholders)-1]
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ","), placeholders)

	for _, row := range td.Rows {
		vals := make([]interface{}, len(cols))
		for i, c := range cols {
			v := row[c]
			switch val := v.(type) {
			case map[string]interface{}, []interface{}:
				b, _ := json.Marshal(val)
				vals[i] = string(b)
			default:
				vals[i] = v
			}
		}
		if _, err := db.Exec(insertSQL, vals...); err != nil {
			return fmt.Errorf("INSERT INTO %s: %w", table, err)
		}
	}

	return nil
}

// CleanupDB removes the ephemeral database file.
func CleanupDB(dbPath string) error {
	return os.Remove(dbPath)
}
