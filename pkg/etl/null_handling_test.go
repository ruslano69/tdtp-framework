package etl

// NULL и пустая строка на пути источник → workspace → результат.
//
// Эталон — обычный SQLite на тех же данных. Раньше workspace считал NULL только
// "" и слово "NULL": настоящий NULL сохранялся непустым текстом (0x00 из БД,
// "[NULL]" из TDTP-файла), а пустая строка становилась NULL. AVG, COUNT,
// IS NULL и фильтры молча считали неверно.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
	"github.com/ruslano69/tdtp-framework/pkg/xlsx"
)

const nullFixtureSQL = `
CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT, amount DECIMAL(12,2), active BOOLEAN,
                qty INTEGER, born DATE, price REAL);
INSERT INTO t VALUES (1,'full',1500.25,1,7,'1990-05-15',2.5);
INSERT INTO t VALUES (2,NULL,NULL,NULL,NULL,NULL,NULL);
INSERT INTO t VALUES (3,'',100.00,0,0,'2000-01-01',0);
INSERT INTO t VALUES (4,'[NULL]',1.00,1,1,'2001-01-01',1);
INSERT INTO t VALUES (5,'NULL',2.00,0,2,'2002-01-01',2);`

type nullSources struct {
	database string
	sources  map[string]SourceConfig
}

// newNullSources — одни и те же строки как SQLite-источник и как TDTP-файл,
// выгруженный обычным --export этой базы.
func newNullSources(t *testing.T) nullSources {
	t.Helper()
	dir := t.TempDir()
	database := filepath.Join(dir, "src.db")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(nullFixtureSQL); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(nil, ErrorHandlingConfig{})
	adapter, err := loader.adapterFor(context.Background(), SourceConfig{Name: "t", Type: "sqlite", DSN: database}, false)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := adapter.ExportTable(context.Background(), "t")
	loader.closeAdapters(context.Background())
	if err != nil || len(packets) != 1 {
		t.Fatalf("export: %d packets, %v", len(packets), err)
	}
	file := filepath.Join(dir, "t.tdtp.xml")
	if err := packet.NewGenerator().WriteToFile(packets[0], file); err != nil {
		t.Fatal(err)
	}

	return nullSources{database: database, sources: map[string]SourceConfig{
		"sqlite": {Name: "t", Type: "sqlite", DSN: database, Query: "SELECT * FROM t ORDER BY id"},
		"tdtp":   {Name: "t", Type: "tdtp", DSN: file},
	}}
}

func runNullPipeline(t *testing.T, source SourceConfig, query string, output OutputConfig, chain ...processors.Config) {
	t.Helper()
	config := &PipelineConfig{
		Name:      "nulls",
		Sources:   []SourceConfig{source},
		Workspace: WorkspaceConfig{Type: "sqlite", Mode: ":memory:"},
		Transform: TransformConfig{SQL: query, ResultTable: "result"},
		Output:    output,
	}
	config.Processors.PreExport = chain
	config.SetDefaults()
	p := NewProcessor(config)
	if err := p.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// pipelineValues возвращает результат пайплайна так, как его прочтёт получатель
// пакета: NULL по объявленному маркеру поля — "∅".
func pipelineValues(t *testing.T, source SourceConfig, query string, chain ...processors.Config) []string {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "out.tdtp.xml")
	runNullPipeline(t, source, query,
		OutputConfig{Type: "tdtp", TDTP: &TDTPOutputConfig{Format: "xml", Destination: destination}}, chain...)
	pkt, err := packet.NewParser().ParseFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, row := range pkt.GetRows() {
		for i, v := range row {
			if v == packet.NullMarkerOf(pkt.Schema.Fields[i]) {
				row[i] = "∅"
			} else if strings.ContainsRune(v, 0) {
				t.Fatalf("raw NUL byte reached the output: %q", row)
			}
		}
		out = append(out, strings.Join(row, ";"))
	}
	return out
}

func sqliteValues(t *testing.T, database, query string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var out []string
	for rows.Next() {
		values := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		parts := make([]string, len(cols))
		for i, v := range values {
			parts[i] = "∅"
			if v.Valid {
				parts[i] = v.String
			}
		}
		out = append(out, strings.Join(parts, ";"))
	}
	return out
}

func TestNull_AggregatesAndPredicatesMatchPlainSQLite(t *testing.T) {
	f := newNullSources(t)
	queries := []string{
		"SELECT AVG(amount), AVG(qty), COUNT(qty), COUNT(amount), COUNT(name), COUNT(active), SUM(qty) FROM t",
		"SELECT SUM(amount IS NULL), SUM(qty IS NULL), SUM(born IS NULL), SUM(active IS NULL), SUM(name IS NULL), SUM(price IS NULL) FROM t",
		"SELECT COUNT(*) FROM t WHERE amount > 0",
		"SELECT COUNT(*) FROM t WHERE name = ''",
		"SELECT id FROM t WHERE name IS NULL",
		"SELECT id FROM t WHERE name = '[NULL]'",
		"SELECT id FROM t WHERE name = 'NULL'",
		"SELECT MIN(qty), MAX(qty), MIN(name) FROM t",
		"SELECT id, COALESCE(qty, -1), COALESCE(active, 9), COALESCE(name, 'none') FROM t ORDER BY id",
		"SELECT COUNT(*) FROM t a JOIN t b ON a.name = b.name",
	}
	for kind, source := range f.sources {
		for _, query := range queries {
			want := sqliteValues(t, f.database, query)
			got := pipelineValues(t, source, query)
			if !slices.Equal(got, want) {
				t.Errorf("source=%s\n  %s\n  got  %q\n  want %q", kind, query, got, want)
			}
		}
	}
}

// NULL, пустая строка и тексты, похожие на маркер, доходят до результата
// разными значениями.
func TestNull_SelectAllKeepsNullAndEmptyApart(t *testing.T) {
	f := newNullSources(t)
	for kind, source := range f.sources {
		got := pipelineValues(t, source, "SELECT id, name, active, qty FROM t ORDER BY id")
		want := []string{"1;full;1;7", "2;∅;∅;∅", "3;;0;0", "4;[NULL];1;1", "5;NULL;0;2"}
		if !slices.Equal(got, want) {
			t.Errorf("source=%s\n got  %q\n want %q", kind, got, want)
		}
	}
}

// NULL, появившийся в transform (LEFT JOIN, вычисление), тоже NULL, а не "".
func TestNull_CreatedByTransform(t *testing.T) {
	f := newNullSources(t)
	for kind, source := range f.sources {
		got := pipelineValues(t, source,
			"SELECT a.id, b.name AS other, NULLIF(a.id, a.id) AS nada, '' AS blank FROM t a LEFT JOIN t b ON b.id = a.id + 100 WHERE a.id = 1")
		if want := []string{"1;∅;∅;"}; !slices.Equal(got, want) {
			t.Errorf("source=%s got %q want %q", kind, got, want)
		}
	}
}

func TestNull_XLSXHasBlankCells(t *testing.T) {
	f := newNullSources(t)
	for kind, source := range f.sources {
		destination := filepath.Join(t.TempDir(), "out.xlsx")
		runNullPipeline(t, source, "SELECT * FROM t ORDER BY id",
			OutputConfig{Type: "xlsx", XLSX: &XLSXOutputConfig{Destination: destination}})
		pkt, err := xlsx.FromXLSX(destination, "")
		if err != nil {
			t.Fatal(err)
		}
		row := pkt.GetRows()[1]
		for i, v := range row[1:] {
			if v != "" {
				t.Errorf("source=%s: NULL cell %s of row 2 holds %q", kind, pkt.Schema.Fields[i+1].Name, v)
			}
		}
	}
}

// Маскирование не должно превращать NULL в звёздочки или текст.
func TestNull_MaskingLeavesNullAlone(t *testing.T) {
	f := newNullSources(t)
	mask := processors.Config{Type: "field_masker", Params: map[string]any{"fields": map[string]any{"name": "stars"}}}
	for kind, source := range f.sources {
		got := pipelineValues(t, source, "SELECT id, name FROM t WHERE id IN (1, 2) ORDER BY id", mask)
		if len(got) != 2 || got[1] != "2;∅" || strings.Contains(got[0], "full") {
			t.Errorf("source=%s got %q", kind, got)
		}
	}
}

// У CSV нет своего обозначения NULL: пустая ячейка и слово NULL, как и раньше,
// читаются как NULL.
func TestNull_CSVKeepsEmptyAsNull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.csv")
	if err := os.WriteFile(path, []byte("id,v\n1,\n2,NULL\n3,x\n4,[NULL]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := SourceConfig{Name: "t", Type: "csv", DSN: path, CSV: &CSVSourceConfig{Columns: []string{"id", "v"}}}
	got := pipelineValues(t, source, "SELECT id, v IS NULL, COALESCE(v, '-') FROM t ORDER BY id")
	if want := []string{"1;1;-", "2;1;-", "3;0;x", "4;0;[NULL]"}; !slices.Equal(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
}

// Потоковое чтение (брокеры) отмечает NULL так же, как пакетное: генератор
// частей превращает метку в объявленный маркер.
func TestNull_StreamMarksNull(t *testing.T) {
	ctx := context.Background()
	w, err := NewWorkspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close(ctx) }()

	stream, err := w.ExecuteSQLStream(ctx, "SELECT NULL AS a, '' AS b", "r")
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := packet.NewStreamingGenerator().GeneratePartsStream(ctx, stream.RowsChan, stream.Schema, "r", packet.TypeReference)
	for part := range parts {
		if part.Error != nil {
			t.Fatal(part.Error)
		}
		row := part.Packet.GetRows()[0]
		if row[0] != packet.NullMarkerOf(part.Packet.Schema.Fields[0]) || row[1] != "" {
			t.Fatalf("got %q", row)
		}
	}
	if err := <-stream.ErrorChan; err != nil {
		t.Fatal(err)
	}
}

// NULL и пустая строка переживают кеш рабочей БД.
func TestNull_SurvivesWorkspaceCache(t *testing.T) {
	f := newNullSources(t)
	dir := t.TempDir()
	destination := filepath.Join(dir, "out.tdtp.xml")
	run := func() ([]string, bool) {
		config := &PipelineConfig{
			Name:    "nulls",
			Sources: []SourceConfig{f.sources["tdtp"]},
			Workspace: WorkspaceConfig{Type: "sqlite", Mode: ":memory:",
				Cache: &WorkspaceCacheConfig{Path: filepath.Join(dir, "ws.zst")}},
			Transform: TransformConfig{SQL: "SELECT id, name IS NULL, name = '', COALESCE(qty, -1) FROM t ORDER BY id", ResultTable: "result"},
			Output:    OutputConfig{Type: "tdtp", TDTP: &TDTPOutputConfig{Format: "xml", Destination: destination}},
		}
		config.SetDefaults()
		p := NewProcessor(config)
		if err := p.Execute(context.Background()); err != nil {
			t.Fatal(err)
		}
		pkt, err := packet.NewParser().ParseFile(destination)
		if err != nil {
			t.Fatal(err)
		}
		var rows []string
		for _, row := range pkt.GetRows()[1:3] {
			rows = append(rows, strings.Join(row, ";"))
		}
		return rows, p.GetStats().CacheHit
	}

	built, hit := run()
	if hit {
		t.Fatal("first run cannot hit the cache")
	}
	restored, hit := run()
	if !hit {
		t.Fatal("second run must hit the cache")
	}
	want := []string{"2;1;" + packet.SpecNullMarker + ";-1", "3;0;1;0"}
	if !slices.Equal(built, want) || !slices.Equal(restored, want) {
		t.Fatalf("built %q, restored %q, want %q", built, restored, want)
	}
}

// NULL — это отсутствие значения: required обязан его отклонить, откуда бы он
// ни взялся. Пустую ячейку от текста "[NULL]" отличает объявленный маркер.
func TestNull_RequiredRejectsNull(t *testing.T) {
	f := newNullSources(t)
	required := processors.Config{Type: "field_validator", Params: map[string]any{
		"rules": map[string]any{"v": "required"}, "on_error": "filter",
	}}
	for kind, source := range f.sources {
		got := pipelineValues(t, source, "SELECT id, name AS v FROM t ORDER BY id", required)
		if want := []string{"1;full", "4;[NULL]", "5;NULL"}; !slices.Equal(got, want) {
			t.Errorf("source=%s, NULL from the source: got %q want %q", kind, got, want)
		}
		got = pipelineValues(t, source,
			"SELECT id, CASE WHEN id = 1 THEN 'x' END AS v FROM t WHERE id <= 3 ORDER BY id", required)
		if want := []string{"1;x"}; !slices.Equal(got, want) {
			t.Errorf("source=%s, NULL from the transform: got %q want %q", kind, got, want)
		}
	}
}
