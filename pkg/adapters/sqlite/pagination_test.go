package sqlite

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// setupPaginationDB создаёт SQLite БД с 10 строками для тестов пагинации.
func setupPaginationDB(t *testing.T) (*Adapter, func()) {
	t.Helper()
	if !isSQLiteDriverAvailable() {
		t.Skip("SQLite driver not available")
	}

	dbFile := t.TempDir() + "/pagination.db"
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE items (id INTEGER, name TEXT, val REAL)`); err != nil {
		db.Close()
		t.Fatalf("create table: %v", err)
	}
	for i := 1; i <= 10; i++ {
		if _, err := db.Exec(`INSERT INTO items VALUES (?, ?, ?)`, i, "item", float64(i)*1.1); err != nil {
			db.Close()
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	db.Close()

	adapter, err := NewAdapter(dbFile)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	cleanup := func() {
		_ = adapter.Close(context.Background())
		os.Remove(dbFile)
	}
	return adapter, cleanup
}

// paginationQuery строит Query с заданными Limit/Offset.
func paginationQuery(limit, offset int) *packet.Query {
	q := packet.NewQuery()
	q.Limit = limit
	q.Offset = offset
	return q
}

// TestPagination_FirstPage — первая страница из 3 строк при total=10.
// MoreDataAvailable=true, NextOffset=3, TotalRecordsInTable=10.
func TestPagination_FirstPage(t *testing.T) {
	adapter, cleanup := setupPaginationDB(t)
	defer cleanup()

	ctx := context.Background()
	packets, err := adapter.ExportTableWithQuery(ctx, "items", paginationQuery(3, 0), "test", "test")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(packets) == 0 {
		t.Fatal("no packets")
	}
	pkt := packets[0]

	rows := pkt.GetRows()
	if len(rows) != 3 {
		t.Errorf("rows: want 3, got %d", len(rows))
	}

	qc := pkt.QueryContext
	if qc == nil {
		t.Fatal("QueryContext is nil")
	}

	if qc.ExecutionResults.TotalRecordsInTable != 10 {
		t.Errorf("TotalRecordsInTable: want 10, got %d", qc.ExecutionResults.TotalRecordsInTable)
	}
	if !qc.ExecutionResults.MoreDataAvailable {
		t.Error("MoreDataAvailable: want true")
	}
	if qc.ExecutionResults.NextOffset != 3 {
		t.Errorf("NextOffset: want 3, got %d", qc.ExecutionResults.NextOffset)
	}
}

// TestPagination_MiddlePage — средняя страница: offset=5, limit=3, total=10.
// MoreDataAvailable=true, NextOffset=8.
func TestPagination_MiddlePage(t *testing.T) {
	adapter, cleanup := setupPaginationDB(t)
	defer cleanup()

	ctx := context.Background()
	packets, err := adapter.ExportTableWithQuery(ctx, "items", paginationQuery(3, 5), "test", "test")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	pkt := packets[0]
	qc := pkt.QueryContext

	rows := pkt.GetRows()
	if len(rows) != 3 {
		t.Errorf("rows: want 3, got %d", len(rows))
	}
	if !qc.ExecutionResults.MoreDataAvailable {
		t.Error("MoreDataAvailable: want true (offset=5+returned=3=8 < total=10)")
	}
	if qc.ExecutionResults.NextOffset != 8 {
		t.Errorf("NextOffset: want 8, got %d", qc.ExecutionResults.NextOffset)
	}
}

// TestPagination_LastPage — последняя страница: offset=8, limit=5, total=10.
// Возвращаются строки 9-10 (2 строки), MoreDataAvailable=false.
func TestPagination_LastPage(t *testing.T) {
	adapter, cleanup := setupPaginationDB(t)
	defer cleanup()

	ctx := context.Background()
	packets, err := adapter.ExportTableWithQuery(ctx, "items", paginationQuery(5, 8), "test", "test")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	pkt := packets[0]
	qc := pkt.QueryContext

	rows := pkt.GetRows()
	if len(rows) != 2 {
		t.Errorf("rows: want 2 (only 2 remain after offset=8), got %d", len(rows))
	}
	if qc.ExecutionResults.MoreDataAvailable {
		t.Error("MoreDataAvailable: want false (last page)")
	}
	if qc.ExecutionResults.TotalRecordsInTable != 10 {
		t.Errorf("TotalRecordsInTable: want 10, got %d", qc.ExecutionResults.TotalRecordsInTable)
	}
}

// TestPagination_ExactLastPage — offset+limit точно совпадает с total.
// LIMIT 5 OFFSET 5 при total=10 → returned=5, offset+returned=10=total → MoreDataAvailable=false.
func TestPagination_ExactLastPage(t *testing.T) {
	adapter, cleanup := setupPaginationDB(t)
	defer cleanup()

	ctx := context.Background()
	packets, err := adapter.ExportTableWithQuery(ctx, "items", paginationQuery(5, 5), "test", "test")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	pkt := packets[0]
	qc := pkt.QueryContext

	rows := pkt.GetRows()
	if len(rows) != 5 {
		t.Errorf("rows: want 5, got %d", len(rows))
	}
	if qc.ExecutionResults.MoreDataAvailable {
		t.Error("MoreDataAvailable: want false (offset+returned == total)")
	}
}

// TestPagination_NoLimit — без пагинации: TotalRecordsInTable должно быть 0
// (оптимизация: GetRowCount не вызывается без Limit).
func TestPagination_NoLimit(t *testing.T) {
	adapter, cleanup := setupPaginationDB(t)
	defer cleanup()

	ctx := context.Background()

	q := packet.NewQuery()
	// Только фильтр, без Limit
	q.Filters = &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "id", Operator: "gt", Value: "3"},
			},
		},
	}

	packets, err := adapter.ExportTableWithQuery(ctx, "items", q, "test", "test")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	pkt := packets[0]
	qc := pkt.QueryContext

	rows := pkt.GetRows()
	if len(rows) != 7 {
		t.Errorf("rows: want 7 (id>3), got %d", len(rows))
	}
	// Без Limit GetRowCount не вызывается → TotalRecordsInTable=0
	if qc.ExecutionResults.TotalRecordsInTable != 0 {
		t.Errorf("TotalRecordsInTable: want 0 (no Limit → GetRowCount skipped), got %d",
			qc.ExecutionResults.TotalRecordsInTable)
	}
	if qc.ExecutionResults.MoreDataAvailable {
		t.Error("MoreDataAvailable: want false (no Limit)")
	}
}

// TestPagination_LimitOnly — только Limit без Offset: первые N строк.
func TestPagination_LimitOnly(t *testing.T) {
	adapter, cleanup := setupPaginationDB(t)
	defer cleanup()

	ctx := context.Background()
	packets, err := adapter.ExportTableWithQuery(ctx, "items", paginationQuery(4, 0), "test", "test")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	pkt := packets[0]
	qc := pkt.QueryContext

	rows := pkt.GetRows()
	if len(rows) != 4 {
		t.Errorf("rows: want 4, got %d", len(rows))
	}
	if !qc.ExecutionResults.MoreDataAvailable {
		t.Error("MoreDataAvailable: want true (4 < 10)")
	}
	if qc.ExecutionResults.NextOffset != 4 {
		t.Errorf("NextOffset: want 4, got %d", qc.ExecutionResults.NextOffset)
	}
}
