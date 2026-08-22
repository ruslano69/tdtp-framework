package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// Большое DECIMAL уезжало в пакет экспонентой: значение печаталось через 'g',
// проверка scale в parseDecimal принимала мантиссу за дробную часть и
// возвращала ошибку, а ConvertValueToTDTP на ошибке отдаёт значение как есть.
//
// Юнит-проверка живёт в pkg/adapters/base и покрывает все dbType сразу; эта
// дублирует её на живом драйвере, потому что SQLite решает тип колонки по её
// объявлению и отдаёт REAL как float64 сам — то есть сюда приходит настоящее
// значение от драйвера, а не сконструированное в тесте. И, в отличие от
// MySQL/MSSQL, она не требует контейнера и идёт в CI всегда.
func TestDecimalNotScientific(t *testing.T) {
	if !isSQLiteDriverAvailable() {
		t.Skip("SQLite driver not available")
	}

	ctx := context.Background()
	dbFile := filepath.Join(t.TempDir(), "dec.db")

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const ddl = `CREATE TABLE dec_test (
		id    INTEGER PRIMARY KEY,
		label TEXT,
		bal   DECIMAL(12,2),
		big   DECIMAL(20,4),
		r     REAL
	)`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		db.Close()
		t.Fatalf("create: %v", err)
	}
	rows := []struct {
		id    int
		label string
		bal   float64
		big   float64
		r     float64
	}{
		{1, "DECIMAL(12,2) limit", 9999999999.99, 1234567890123456.7891, 1e20},
		{2, "negative limit", -9999999999.99, -1234567890123456.7891, -1e20},
		{3, "integer past 1e8", 486789500, 486789500, 486789500},
		{4, "ordinary", 1500.5, 0.5, 0.000000123},
		{5, "zero", 0, 0, 0},
	}
	for _, r := range rows {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO dec_test (id, label, bal, big, r) VALUES (?, ?, ?, ?, ?)",
			r.id, r.label, r.bal, r.big, r.r); err != nil {
			db.Close()
			t.Fatalf("insert %d: %v", r.id, err)
		}
	}
	db.Close()

	adapter, err := adapters.New(ctx, adapters.Config{Type: "sqlite", DSN: dbFile})
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	defer adapter.Close(ctx)

	packets, err := adapter.ExportTable(ctx, "dec_test")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(packets) == 0 {
		t.Fatal("no packets exported")
	}

	schema := packets[0].Schema
	got := packets[0].GetRows()
	if len(got) != len(rows) {
		t.Fatalf("exported %d rows, want %d", len(got), len(rows))
	}

	for i, row := range got {
		for c, cell := range row {
			if strings.ContainsAny(cell, "eE") && schema.Fields[c].Name != "label" {
				t.Errorf("row %d (%s), column %s: scientific notation in the packet: %q",
					i+1, rows[i].label, schema.Fields[c].Name, cell)
			}
		}
	}

	// Точные значения у границы DECIMAL(12,2) обязаны доехать целиком.
	if got[0][2] != "9999999999.99" {
		t.Errorf("DECIMAL(12,2) at its limit: got %q, want %q", got[0][2], "9999999999.99")
	}
	if got[1][2] != "-9999999999.99" {
		t.Errorf("negative DECIMAL(12,2) at its limit: got %q, want %q", got[1][2], "-9999999999.99")
	}
	// Обычные значения не должны были измениться вовсе.
	if got[3][2] != "1500.5" {
		t.Errorf("ordinary value changed: got %q, want %q", got[3][2], "1500.5")
	}
}
