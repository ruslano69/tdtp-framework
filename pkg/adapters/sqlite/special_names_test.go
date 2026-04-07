package sqlite

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// TestSpecialTableAndFieldNames verifies that bracket-quoted table names
// (MSSQL-style) with Cyrillic characters, spaces, and # do not get
// double-quoted by the SQLite adapter after StripBrackets unwraps them.
//
// Table name : "кривім названием#"   (passed as "[кривім названием#]")
// Field names: "Код", "Прізвище та імя", "Дата прийому", "Сума грн"
func TestSpecialTableAndFieldNames(t *testing.T) {
	if !isSQLiteDriverAvailable() {
		t.Skip("SQLite driver not available")
	}

	ctx := context.Background()
	dbFile := "testdata/special_names_test.db"
	t.Cleanup(func() { os.Remove(dbFile) })

	adapter, err := NewAdapter(dbFile)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	defer adapter.Close(ctx)

	const rawTableName = "кривім названием#"
	const bracketedName = "[кривім названием#]"

	// Create table with special name and space-containing field names via raw SQL.
	_, err = adapter.db.ExecContext(ctx, `
		CREATE TABLE "кривім названием#" (
			"Код"             INTEGER PRIMARY KEY,
			"Прізвище та імя" TEXT    NOT NULL,
			"Дата прийому"    TEXT,
			"Сума грн"        REAL
		)`)
	if err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}

	_, err = adapter.db.ExecContext(ctx, `
		INSERT INTO "кривім названием#"
			("Код", "Прізвище та імя", "Дата прийому", "Сума грн")
		VALUES
			(1, 'ЧЕРКАСОВ Іван',   '2024-01-15', 5000.00),
			(2, 'ЧЕРКАСОВА Олена', '2024-03-01', 6200.50),
			(3, 'БОНДАРЕНКО Петро','2023-11-10', 4800.00),
			(4, 'МЕЛЬНИК Ганна',   '2025-01-20', 7100.00)`)
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	t.Run("StripBrackets unwraps correctly", func(t *testing.T) {
		got := tdtql.StripBrackets(bracketedName)
		if got != rawTableName {
			t.Errorf("StripBrackets(%q) = %q, want %q", bracketedName, got, rawTableName)
		}
	})

	t.Run("GetTableSchema via bracket name", func(t *testing.T) {
		s, err := adapter.GetTableSchema(ctx, bracketedName)
		if err != nil {
			t.Fatalf("GetTableSchema: %v", err)
		}
		if len(s.Fields) != 4 {
			t.Errorf("expected 4 fields, got %d", len(s.Fields))
		}
		names := make([]string, len(s.Fields))
		for i, f := range s.Fields {
			names[i] = f.Name
		}
		for _, want := range []string{"Код", "Прізвище та імя", "Дата прийому", "Сума грн"} {
			found := false
			for _, n := range names {
				if n == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("field %q not found in schema; got %v", want, names)
			}
		}
	})

	t.Run("GetRowCount via bracket name", func(t *testing.T) {
		count, err := adapter.GetRowCount(ctx, bracketedName)
		if err != nil {
			t.Fatalf("GetRowCount: %v", err)
		}
		if count != 4 {
			t.Errorf("expected 4 rows, got %d", count)
		}
	})

	t.Run("ReadAllRows via bracket name", func(t *testing.T) {
		s, err := adapter.GetTableSchema(ctx, bracketedName)
		if err != nil {
			t.Fatalf("GetTableSchema: %v", err)
		}
		rows, err := adapter.ReadAllRows(ctx, bracketedName, s)
		if err != nil {
			t.Fatalf("ReadAllRows: %v", err)
		}
		if len(rows) != 4 {
			t.Errorf("expected 4 rows, got %d", len(rows))
		}
	})

	t.Run("ExportTableWithQuery LIKE filter via bracket name", func(t *testing.T) {
		translator := tdtql.NewTranslator()
		query, err := translator.Translate(`SELECT * FROM t WHERE [Прізвище та імя] LIKE '%ЧЕРКАСОВ%'`)
		if err != nil {
			t.Fatalf("Translate: %v", err)
		}

		packets, err := adapter.ExportTableWithQuery(ctx, bracketedName, query, "Test", "Test")
		if err != nil {
			t.Fatalf("ExportTableWithQuery: %v", err)
		}
		if len(packets) == 0 {
			t.Fatal("no packets returned")
		}
		got := packets[0].QueryContext.ExecutionResults.RecordsReturned
		if got != 2 {
			t.Errorf("expected 2 ЧЕРКАСОВ rows, got %d", got)
		}
		// Verify neither row contains double-bracket artifacts in serialized data.
		for _, row := range packets[0].Data.Rows {
			if strings.Contains(row.Value, "[[") || strings.Contains(row.Value, "]]") {
				t.Errorf("double-bracket artifact in row value: %s", row.Value)
			}
		}
	})

	t.Run("ExportTableWithQuery numeric filter via bracket name", func(t *testing.T) {
		translator := tdtql.NewTranslator()
		query, err := translator.Translate(`SELECT * FROM t WHERE [Сума грн] > 5000`)
		if err != nil {
			t.Fatalf("Translate: %v", err)
		}

		packets, err := adapter.ExportTableWithQuery(ctx, bracketedName, query, "Test", "Test")
		if err != nil {
			t.Fatalf("ExportTableWithQuery: %v", err)
		}
		got := packets[0].QueryContext.ExecutionResults.RecordsReturned
		if got != 2 {
			t.Errorf("expected 2 rows with Сума грн > 5000, got %d", got)
		}
	})
}
