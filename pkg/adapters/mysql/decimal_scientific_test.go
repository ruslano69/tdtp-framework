package mysql

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// MySQL этой поломкой НЕ задет, и тест стоит здесь именно затем, чтобы это
// зафиксировать, а не затем, что он её ловит: он проходит и на коде до
// исправления.
//
// Причина в драйвере. DECIMAL приезжает []uint8'ом, то есть текстом, и до
// ветки с float64 в genericValueToString дело не доходит вовсе — а печать
// через 'g' и разбор мантиссы как дробной части случались только там. DOUBLE и
// FLOAT приезжают числами, но отображаются в REAL, у которого проверки scale
// нет.
//
// Значит, тест держит не регрессию формата, а допущение о драйвере: если он
// когда-нибудь начнёт отдавать DECIMAL числом, большое значение снова окажется
// на пути, где эта поломка возможна, и тест это заметит. Сама поломка
// воспроизводится на PostgreSQL и SQLite — там регрессионные тесты падают на
// старом коде.
func TestDecimalNotScientific(t *testing.T) {
	ctx := context.Background()
	adapter, err := adapters.New(ctx, adapters.Config{Type: "mysql", DSN: testDSN()})
	if err != nil {
		t.Skipf("MySQL not available: %v", err)
	}
	defer adapter.Close(ctx)

	const tbl = "tdtp_decimal_scientific"
	stmts := []string{
		"DROP TABLE IF EXISTS " + tbl,
		`CREATE TABLE ` + tbl + ` (
			id    INT PRIMARY KEY,
			label VARCHAR(64),
			bal   DECIMAL(12,2),
			big   DECIMAL(20,4),
			d     DOUBLE,
			r     FLOAT
		)`,
		`INSERT INTO ` + tbl + ` VALUES
			(1, 'limit',            9999999999.99,  1234567890123456.7891,  1e20,  4867895),
			(2, 'negative limit',  -9999999999.99, -1234567890123456.7891, -1e20, -4867895),
			(3, 'integer past 1e8',    486789500,             486789500.0,  486789500, 486789),
			(4, 'ordinary',               1500.50,                  0.5000,  0.000000123, 1500.5),
			(5, 'zero',                      0.00,                  0.0000,  0,     0)`,
	}
	db, err := sql.Open("mysql", testDSN())
	if err != nil {
		t.Skipf("MySQL not available: %v", err)
	}
	defer db.Close()
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("setup (%.40s): %v", q, err)
		}
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + tbl) })

	packets, err := adapter.ExportTable(ctx, tbl)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(packets) == 0 {
		t.Fatal("no packets exported")
	}
	schema := packets[0].Schema
	rows := packets[0].GetRows()
	if len(rows) != 5 {
		t.Fatalf("exported %d rows, want 5", len(rows))
	}

	for i, row := range rows {
		for c, cell := range row {
			if schema.Fields[c].Name == "label" {
				continue
			}
			if strings.ContainsAny(cell, "eE") {
				t.Errorf("row %d, column %s: scientific notation in the packet: %q",
					i+1, schema.Fields[c].Name, cell)
			}
		}
	}
	if rows[0][2] != "9999999999.99" {
		t.Errorf("DECIMAL(12,2) at its limit: got %q, want %q", rows[0][2], "9999999999.99")
	}
	if rows[1][2] != "-9999999999.99" {
		t.Errorf("negative DECIMAL(12,2) at its limit: got %q, want %q", rows[1][2], "-9999999999.99")
	}
}
