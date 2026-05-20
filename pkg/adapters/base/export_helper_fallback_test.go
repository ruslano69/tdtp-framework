package base

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// --- mocks ---

type mockSchemaReader struct {
	schema packet.Schema
}

func (m *mockSchemaReader) GetTableSchema(_ context.Context, _ string) (packet.Schema, error) {
	return m.schema, nil
}

type mockDataReader struct {
	sqlErr           error
	rowCount         int64
	rowCountErr      error
	rowsFromSQL      [][]string
	rowsFromAll      [][]string
	readAllRowsCalls int
	getRowCountCalls int
	readSQLCalls     int
}

func (m *mockDataReader) ReadAllRows(_ context.Context, _ string, _ packet.Schema) ([][]string, error) {
	m.readAllRowsCalls++
	return m.rowsFromAll, nil
}

func (m *mockDataReader) ReadRowsWithSQL(_ context.Context, _ string, _ packet.Schema) ([][]string, error) {
	m.readSQLCalls++
	if m.sqlErr != nil {
		return nil, m.sqlErr
	}
	return m.rowsFromSQL, nil
}

func (m *mockDataReader) GetRowCount(_ context.Context, _ string) (int64, error) {
	m.getRowCountCalls++
	return m.rowCount, m.rowCountErr
}

type mockValueConverter struct{}

func (m *mockValueConverter) ConvertValueToTDTP(_ packet.Field, value string) string { return value }

// --- helpers ---

func buildFallbackTestHelper(reader *mockDataReader) *ExportHelper {
	s := schema.NewBuilder().AddInteger("ID", true).AddText("Name", 100).Build()
	return NewExportHelper(
		&mockSchemaReader{schema: s},
		reader,
		&mockValueConverter{},
		nil, // sqlAdapter optional
	)
}

func buildEqQuery() *packet.Query {
	return &packet.Query{
		Filters: &packet.Filters{
			And: &packet.LogicalGroup{
				Filters: []packet.Filter{{Field: "ID", Operator: "eq", Value: "42"}},
			},
		},
	}
}

// --- tests ---

// Pushdown работает → лимит вообще не проверяется (GetRowCount не зовётся,
// fast path не должен платить за safety-net).
func TestExportHelper_Fallback_PushdownSuccess_SkipsRowCountCheck(t *testing.T) {
	reader := &mockDataReader{
		rowsFromSQL: [][]string{{"42", "Alice"}},
		rowCount:    999_999_999, // огромное, но мы не должны его прочитать
	}
	helper := buildFallbackTestHelper(reader)
	helper.SetMaxFallbackRows(1000)

	_, err := helper.ExportTableWithQuery(context.Background(), "Users", buildEqQuery(), "test", "test")
	if err != nil {
		t.Fatalf("expected pushdown success, got error: %v", err)
	}
	if reader.getRowCountCalls != 0 {
		t.Errorf("GetRowCount must NOT be called on pushdown success path, got %d calls", reader.getRowCountCalls)
	}
	if reader.readAllRowsCalls != 0 {
		t.Errorf("ReadAllRows must NOT be called on pushdown success path, got %d calls", reader.readAllRowsCalls)
	}
}

// Pushdown упал, таблица больше лимита → ошибка, ReadAllRows не зовётся.
// Это центральная защита от 17 GB сканов на проде.
func TestExportHelper_Fallback_OverLimit_AbortsBeforeReadAllRows(t *testing.T) {
	reader := &mockDataReader{
		sqlErr:   errors.New("mssql: Conversion failed"),
		rowCount: 24_000_000,
	}
	helper := buildFallbackTestHelper(reader)
	helper.SetMaxFallbackRows(1_000_000)

	_, err := helper.ExportTableWithQuery(context.Background(), "ZTR$Timesheet Line", buildEqQuery(), "test", "test")
	if err == nil {
		t.Fatal("expected fallback abort error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "fallback aborted") {
		t.Errorf("error must say 'fallback aborted', got: %s", msg)
	}
	if !strings.Contains(msg, "24000000") {
		t.Errorf("error must report actual row count (24000000), got: %s", msg)
	}
	if !strings.Contains(msg, "1000000") {
		t.Errorf("error must report the limit (1000000), got: %s", msg)
	}
	if reader.readAllRowsCalls != 0 {
		t.Errorf("ReadAllRows must NOT be called when over limit, got %d calls", reader.readAllRowsCalls)
	}
	if reader.getRowCountCalls != 1 {
		t.Errorf("GetRowCount must be called exactly once, got %d", reader.getRowCountCalls)
	}
}

// Pushdown упал, таблица в пределах лимита → fallback отрабатывает как раньше.
func TestExportHelper_Fallback_UnderLimit_FallsThrough(t *testing.T) {
	reader := &mockDataReader{
		sqlErr:      errors.New("mssql: Conversion failed"),
		rowCount:    500,
		rowsFromAll: [][]string{{"42", "Alice"}, {"7", "Bob"}},
	}
	helper := buildFallbackTestHelper(reader)
	helper.SetMaxFallbackRows(1_000_000)

	_, err := helper.ExportTableWithQuery(context.Background(), "SmallTable", buildEqQuery(), "test", "test")
	if err != nil {
		t.Fatalf("expected fallback to succeed for small table, got error: %v", err)
	}
	if reader.readAllRowsCalls != 1 {
		t.Errorf("ReadAllRows must be called once, got %d", reader.readAllRowsCalls)
	}
}

// maxFallbackRows == 0 (дефолт «без лимита») → проверка отключена, legacy поведение.
func TestExportHelper_Fallback_ZeroLimit_DisablesCheck(t *testing.T) {
	reader := &mockDataReader{
		sqlErr:      errors.New("mssql: Conversion failed"),
		rowCount:    999_999_999, // не должен быть прочитан вообще
		rowsFromAll: [][]string{{"42", "Alice"}},
	}
	helper := buildFallbackTestHelper(reader)
	// SetMaxFallbackRows не вызываем — остаётся 0

	_, err := helper.ExportTableWithQuery(context.Background(), "AnyTable", buildEqQuery(), "test", "test")
	if err != nil {
		t.Fatalf("zero limit must skip check, got error: %v", err)
	}
	if reader.getRowCountCalls != 0 {
		t.Errorf("GetRowCount must NOT be called when limit=0, got %d calls", reader.getRowCountCalls)
	}
	if reader.readAllRowsCalls != 1 {
		t.Errorf("ReadAllRows must be called once on legacy fallback, got %d", reader.readAllRowsCalls)
	}
}

// GetRowCount сам упал → лимит не применяем, продолжаем в fallback.
// Лучше выполнить запрос (вдруг таблица маленькая) чем падать на провале метаданных.
func TestExportHelper_Fallback_RowCountError_DoesNotAbort(t *testing.T) {
	reader := &mockDataReader{
		sqlErr:      errors.New("mssql: Conversion failed"),
		rowCountErr: errors.New("permission denied on sys.partitions"),
		rowsFromAll: [][]string{{"42", "Alice"}},
	}
	helper := buildFallbackTestHelper(reader)
	helper.SetMaxFallbackRows(1_000_000)

	_, err := helper.ExportTableWithQuery(context.Background(), "AnyTable", buildEqQuery(), "test", "test")
	if err != nil {
		t.Fatalf("GetRowCount failure must not abort fallback, got error: %v", err)
	}
	if reader.readAllRowsCalls != 1 {
		t.Errorf("ReadAllRows must still run when row count is unknown, got %d calls", reader.readAllRowsCalls)
	}
}
