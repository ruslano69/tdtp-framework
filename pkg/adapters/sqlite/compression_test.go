package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// TestCompression_RawRowsPath — регрессионный тест на баг:
// после ExportTable пакет содержит rawRows (fast-path GenerateReference).
// Если перед сжатием не вызвать MaterializeRows(), writePacketTo берёт rawRows
// и пишет несжатые данные, игнорируя сжатый Data.Rows.
func TestCompression_RawRowsPath(t *testing.T) {
	// 1. Создаём SQLite БД с тестовыми данными
	dbFile := t.TempDir() + "/compress_test.db"
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE items (id INTEGER, name TEXT, value REAL)`,
		`INSERT INTO items VALUES (1, 'Alpha', 1.1)`,
		`INSERT INTO items VALUES (2, 'Beta',  2.2)`,
		`INSERT INTO items VALUES (3, 'Gamma', 3.3)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatalf("setup: %v", err)
		}
	}
	db.Close()

	// 2. Экспортируем — пакет будет с rawRows (GenerateReference fast-path)
	ctx := context.Background()
	adapter, err := NewAdapter(dbFile)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	pkts, err := adapter.ExportTable(ctx, "items")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(pkts) == 0 {
		t.Fatal("no packets exported")
	}
	pkt := pkts[0]

	// 3. Убеждаемся что rawRows заполнены (fast-path активен)
	if pkt.Header.RecordsInPart != 3 {
		t.Fatalf("expected 3 records, got %d", pkt.Header.RecordsInPart)
	}

	// 4. MaterializeRows() — именно это должно быть в compressPacketData
	pkt.MaterializeRows()
	if len(pkt.Data.Rows) != 3 {
		t.Fatalf("after MaterializeRows: expected 3 Data.Rows, got %d", len(pkt.Data.Rows))
	}

	// 5. Сжимаем (имитируем compressPacketData из export.go)
	rows := make([]string, len(pkt.Data.Rows))
	for i, row := range pkt.Data.Rows {
		rows[i] = row.Value
	}
	compressed, _, err := processors.CompressDataForTdtpAlgo(rows, "zstd", 3)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	pkt.Data.Compression = "zstd"
	pkt.Data.Rows = []packet.Row{{Value: compressed}}

	// 6. Сериализуем в XML
	gen := packet.NewGenerator()
	xmlData, err := gen.ToXML(pkt, true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}

	xmlStr := string(xmlData)

	// 7. Проверяем что XML содержит атрибут compression="zstd"
	if !strings.Contains(xmlStr, `compression="zstd"`) {
		t.Errorf("XML does not contain compression attribute — raw data was written instead of compressed")
	}

	// 8. Проверяем что в XML ровно одна строка данных (blob), а не 3 строки
	rowCount := strings.Count(xmlStr, "<R>")
	if rowCount != 1 {
		t.Errorf("expected 1 compressed row in XML, got %d rows — raw rawRows were written", rowCount)
	}

	// 9. Парсим обратно и декомпрессируем — проверяем данные целы
	parsed, err := packet.NewParser().ParseBytes(xmlData)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Data.Compression != "zstd" {
		t.Errorf("parsed packet has no compression, got %q", parsed.Data.Compression)
	}

	decompRows, err := processors.DecompressDataForTdtpWithAlgo(parsed.Data.Rows[0].Value, "zstd")
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if len(decompRows) != 3 {
		t.Errorf("expected 3 rows after decompress, got %d", len(decompRows))
	}
}

// TestCompression_RawRowsPath_Regression проверяет что БЕЗ MaterializeRows
// баг воспроизводится (сырые данные вместо сжатых).
func TestCompression_RawRowsPath_Regression(t *testing.T) {
	dbFile := t.TempDir() + "/compress_regression.db"
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE items (id INTEGER, name TEXT, value REAL)`,
		`INSERT INTO items VALUES (1, 'Alpha', 1.1)`,
		`INSERT INTO items VALUES (2, 'Beta',  2.2)`,
		`INSERT INTO items VALUES (3, 'Gamma', 3.3)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatalf("setup: %v", err)
		}
	}
	db.Close()

	ctx := context.Background()
	adapter, err := NewAdapter(dbFile)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	pkts, err := adapter.ExportTable(ctx, "items")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	pkt := pkts[0]

	// Имитируем старый баг: SetRows(GetRows()) не очищает rawRows
	// После этого rawRows != nil, и writePacketTo пишет их вместо сжатых данных
	pkt.SetRows(pkt.GetRows()) // старый код без MaterializeRows
	rows := make([]string, len(pkt.Data.Rows))
	for i, row := range pkt.Data.Rows {
		rows[i] = row.Value
	}
	compressed, _, err := processors.CompressDataForTdtpAlgo(rows, "zstd", 3)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	pkt.Data.Compression = "zstd"
	pkt.Data.Rows = []packet.Row{{Value: compressed}}

	gen := packet.NewGenerator()
	xmlData, err := gen.ToXML(pkt, true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}

	xmlStr := string(xmlData)

	// БАГ: при наличии rawRows — в XML пишутся 3 несжатые строки вместо 1 сжатой
	rowCount := strings.Count(xmlStr, "<R>")
	if rowCount == 1 {
		t.Log("SetRows(GetRows()) case: 1 row — rawRows were nil (no bug in this build)")
	} else {
		t.Logf("SetRows(GetRows()) case: %d rows written — rawRows not cleared (bug confirmed)", rowCount)
	}
	// Не фейлим — это документационный тест поведения
}
