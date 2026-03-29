package etl

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	sqliteadapter "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	_ "modernc.org/sqlite"
)

// TestWorkspace_LoadData_FromExport проверяет что LoadData корректно работает
// с пакетами из ExportTable (rawRows fast-path), а не только с Data.Rows.
func TestWorkspace_LoadData_FromExport(t *testing.T) {
	ctx := context.Background()

	// 1. Создаём файловый SQLite источник с тестовыми данными
	dbFile := t.TempDir() + "/orders.db"
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, product TEXT, amount REAL, active INTEGER)`,
		`INSERT INTO orders VALUES (1, 'Apple',  100.0, 1)`,
		`INSERT INTO orders VALUES (2, 'Banana', 200.0, 1)`,
		`INSERT INTO orders VALUES (3, 'Cherry',  50.0, 0)`,
		`INSERT INTO orders VALUES (4, 'Date',   300.0, 1)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatalf("setup: %v", err)
		}
	}
	db.Close()

	src, err := sqliteadapter.NewAdapter(dbFile)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	defer src.Close(ctx)

	// 2. Экспортируем — пакет будет с rawRows (GenerateReference fast-path)
	packets, err := src.ExportTable(ctx, "orders")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(packets) == 0 {
		t.Fatal("no packets")
	}
	pkt := packets[0]

	if pkt.Header.RecordsInPart != 4 {
		t.Fatalf("expected 4 rows in packet, got %d", pkt.Header.RecordsInPart)
	}

	// 3. Создаём workspace и загружаем данные
	ws, err := NewWorkspace(ctx)
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	defer ws.Close(ctx)

	if err := ws.CreateTable(ctx, "orders", pkt.Schema.Fields); err != nil {
		t.Fatalf("create table: %v", err)
	}

	if err := ws.LoadData(ctx, "orders", pkt); err != nil {
		t.Fatalf("load data: %v", err)
	}

	// 4. Выполняем SQL и проверяем результат
	result, err := ws.ExecuteSQL(ctx,
		"SELECT product, amount FROM orders WHERE active = 1 ORDER BY amount DESC",
		"result")
	if err != nil {
		t.Fatalf("execute SQL: %v", err)
	}

	rows := result.GetRows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows after filter, got %d", len(rows))
	}

	// Проверяем порядок (ORDER BY amount DESC: Date=300, Banana=200, Apple=100)
	expected := [][]string{
		{"Date", "300"},
		{"Banana", "200"},
		{"Apple", "100"},
	}
	for i, row := range rows {
		if row[0] != expected[i][0] {
			t.Errorf("row %d product: got %q, want %q", i, row[0], expected[i][0])
		}
	}
}

// BenchmarkWorkspace_LoadData тестирует LoadData + ExecuteSQL на N строках in-memory SQLite.
func BenchmarkWorkspace_LoadData(b *testing.B) {
	const N = 100_000
	ctx := context.Background()

	// Один раз строим пакет с N строками (имитируем экспорт из источника)
	fields := []packet.Field{
		{Name: "ID", Type: "INTEGER"},
		{Name: "Name", Type: "TEXT"},
		{Name: "City", Type: "TEXT"},
		{Name: "Balance", Type: "REAL"},
		{Name: "IsActive", Type: "INTEGER"},
		{Name: "RegisteredAt", Type: "TEXT"},
	}
	schema := packet.Schema{Fields: fields}
	rawRows := make([][]string, N)
	cities := []string{"Moscow", "Saint Petersburg", "Novosibirsk", "Kazan", "Samara"}
	for i := 0; i < N; i++ {
		rawRows[i] = []string{
			fmt.Sprintf("%d", i+1),
			fmt.Sprintf("User %d", i),
			cities[i%len(cities)],
			fmt.Sprintf("%.2f", float64(i%100000)),
			fmt.Sprintf("%d", i%2),
			"2024-01-01 00:00:00",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ws, err := NewWorkspace(ctx)
		if err != nil {
			b.Fatalf("workspace: %v", err)
		}
		if err := ws.CreateTable(ctx, "users", fields); err != nil {
			ws.Close(ctx)
			b.Fatalf("create table: %v", err)
		}

		pkt := packet.NewDataPacket(packet.TypeReference, "users")
		pkt.Schema = schema
		pkt.Header.RecordsInPart = N
		// Прямо в Data.Rows — без rawRows, честный INSERT benchmark
		rows := make([]packet.Row, N)
		for j, r := range rawRows {
			var sb string
			for k, v := range r {
				if k > 0 {
					sb += "|"
				}
				sb += v
			}
			rows[j] = packet.Row{Value: sb}
		}
		pkt.Data.Rows = rows

		start := time.Now()
		if err := ws.LoadData(ctx, "users", pkt); err != nil {
			ws.Close(ctx)
			b.Fatalf("load: %v", err)
		}
		result, err := ws.ExecuteSQL(ctx,
			"SELECT ID, Name, Balance FROM users WHERE IsActive = 1 AND Balance > 50000 ORDER BY Balance DESC LIMIT 1000",
			"result")
		elapsed := time.Since(start)
		if err != nil {
			ws.Close(ctx)
			b.Fatalf("sql: %v", err)
		}

		colCount := len(fields)
		b.ReportMetric(float64(N*colCount)/elapsed.Seconds()/1e6, "Mfields/s")
		_ = result
		ws.Close(ctx)
	}
}

// TestWorkspace_LoadData_EmptyPacket проверяет что пустой пакет обрабатывается без ошибок.
func TestWorkspace_LoadData_EmptyPacket(t *testing.T) {
	ctx := context.Background()

	ws, err := NewWorkspace(ctx)
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	defer ws.Close(ctx)

	fields := []packet.Field{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
	}
	if err := ws.CreateTable(ctx, "empty", fields); err != nil {
		t.Fatalf("create: %v", err)
	}

	empty := packet.NewDataPacket(packet.TypeReference, "empty")
	empty.Schema.Fields = fields

	if err := ws.LoadData(ctx, "empty", empty); err != nil {
		t.Fatalf("load empty: %v", err)
	}

	result, err := ws.ExecuteSQL(ctx, "SELECT COUNT(*) FROM empty", "cnt")
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	rows := result.GetRows()
	if len(rows) != 1 || rows[0][0] != "0" {
		t.Fatalf("expected count=0, got %v", rows)
	}
}
