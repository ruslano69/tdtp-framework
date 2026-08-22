package mssql

import (
	"context"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// MS SQL Server этой поломкой НЕ задет, и тест стоит здесь именно затем, чтобы
// это зафиксировать, а не затем, что он её ловит: он проходит и на коде до
// исправления.
//
// Причина та же, что и у MySQL: DECIMAL приезжает []uint8'ом, то есть текстом,
// и до ветки с float64 в mssqlValueToString дело не доходит. FLOAT и REAL
// приезжают float64, но отображаются в REAL, у которого проверки scale нет.
//
// Значит, тест держит допущение о драйвере, а не регрессию формата. Сама
// поломка воспроизводится на PostgreSQL и SQLite — там регрессионные тесты
// падают на старом коде.
func TestDecimalNotScientific(t *testing.T) {
	ctx := context.Background()

	adapter, err := adapters.New(ctx, adapters.Config{Type: "mssql", DSN: testConnStringDev})
	if err != nil {
		t.Skipf("MS SQL Server not available: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close(ctx) })

	mssqlAdapter, ok := adapter.(*Adapter)
	if !ok {
		t.Fatal("adapter is not *mssql.Adapter")
	}

	const tbl = "tdtp_decimal_scientific"
	stmts := []string{
		"IF OBJECT_ID('" + tbl + "', 'U') IS NOT NULL DROP TABLE " + tbl,
		`CREATE TABLE ` + tbl + ` (
			id    INT PRIMARY KEY,
			label NVARCHAR(64),
			bal   DECIMAL(12,2),
			big   DECIMAL(20,4),
			d     FLOAT,
			r     REAL
		)`,
		`INSERT INTO ` + tbl + ` VALUES
			(1, 'limit',            9999999999.99,  1234567890123456.7891,  1e20,  4867895),
			(2, 'negative limit',  -9999999999.99, -1234567890123456.7891, -1e20, -4867895),
			(3, 'integer past 1e8',    486789500,             486789500.0,  486789500, 486789),
			(4, 'ordinary',              1500.50,                  0.5000,  0.000000123, 1500.5),
			(5, 'zero',                     0.00,                  0.0000,  0,     0)`,
	}
	for _, q := range stmts {
		if _, err := mssqlAdapter.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("setup (%.40s): %v", q, err)
		}
	}
	t.Cleanup(func() {
		_, _ = mssqlAdapter.db.ExecContext(context.Background(),
			"IF OBJECT_ID('"+tbl+"', 'U') IS NOT NULL DROP TABLE "+tbl)
	})

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
