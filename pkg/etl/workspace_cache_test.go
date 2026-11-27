package etl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func writeCacheSource(t *testing.T, path string, rows [][]string) {
	t.Helper()
	pkt := packet.NewDataPacket(packet.TypeReference, "users")
	pkt.Schema = packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "city", Type: "TEXT", Length: 40},
		{Name: "balance", Type: "DECIMAL", Precision: 12, Scale: 2},
		{Name: "active", Type: "BOOLEAN"},
		{Name: "born", Type: "DATE"},
	}}
	pkt.Data = packet.RowsToData(rows)
	pkt.Header.RecordsInPart = len(rows)
	if err := packet.NewGenerator().WriteToFile(pkt, path); err != nil {
		t.Fatal(err)
	}
}

func cacheRows() [][]string {
	return [][]string{
		{"1", "Omsk", "100.50", "1", "1990-05-15"},
		{"2", "Ufa", "200.00", "0", "1985-01-01"},
		{"3", "Omsk", "300.25", "1", "2000-02-29"},
		{"4", "", "0.00", "0", ""},
	}
}

type cacheFixture struct {
	dir, source, cache, output string
}

func newCacheFixture(t *testing.T) cacheFixture {
	t.Helper()
	dir := t.TempDir()
	f := cacheFixture{
		dir:    dir,
		source: filepath.Join(dir, "users.tdtp.xml"),
		cache:  filepath.Join(dir, "cache", "ws.zst"),
		output: filepath.Join(dir, "out.tdtp.xml"),
	}
	writeCacheSource(t, f.source, cacheRows())
	return f
}

func (f cacheFixture) config(cached bool, indexes ...WorkspaceIndexConfig) *PipelineConfig {
	c := &PipelineConfig{
		Name:      "cache-test",
		Sources:   []SourceConfig{{Name: "users", Type: "tdtp", DSN: f.source}},
		Workspace: WorkspaceConfig{Type: "sqlite", Mode: ":memory:"},
		Transform: TransformConfig{SQL: "SELECT * FROM users WHERE city = 'Omsk' ORDER BY id", ResultTable: "result"},
		Output:    OutputConfig{Type: "tdtp", TDTP: &TDTPOutputConfig{Format: "xml", Destination: f.output}},
	}
	if cached {
		c.Workspace.Cache = &WorkspaceCacheConfig{Path: f.cache, Indexes: indexes}
	}
	c.SetDefaults()
	return c
}

type cacheRun struct {
	stats  ProcessorStats
	schema packet.Schema
	rows   [][]string
}

func (f cacheFixture) run(t *testing.T, config *PipelineConfig) cacheRun {
	t.Helper()
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	processor := NewProcessor(config)
	if err := processor.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if errs := processor.GetStats().Errors; len(errs) > 0 {
		t.Fatalf("pipeline errors: %v", errs)
	}
	out, err := packet.NewParser().ParseFile(f.output)
	if err != nil {
		t.Fatal(err)
	}
	// Временные файлы с открытой копией базы не должны переживать запуск.
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(f.cache), workspaceCachePrefix+"*"))
	if len(left) > 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
	return cacheRun{stats: processor.GetStats(), schema: out.Schema, rows: out.GetRows()}
}

func assertSameResult(t *testing.T, got, want cacheRun) {
	t.Helper()
	if !reflect.DeepEqual(got.rows, want.rows) {
		t.Fatalf("rows differ:\n got %q\nwant %q", got.rows, want.rows)
	}
	if !reflect.DeepEqual(got.schema, want.schema) {
		t.Fatalf("schema differs:\n got %+v\nwant %+v", got.schema, want.schema)
	}
	if got.stats.TotalRowsLoaded != want.stats.TotalRowsLoaded || got.stats.SourcesLoaded != want.stats.SourcesLoaded {
		t.Fatalf("stats differ: got %d rows/%d sources, want %d/%d", got.stats.TotalRowsLoaded,
			got.stats.SourcesLoaded, want.stats.TotalRowsLoaded, want.stats.SourcesLoaded)
	}
}

func TestWorkspaceCache_HitGivesSameResult(t *testing.T) {
	f := newCacheFixture(t)
	index := WorkspaceIndexConfig{Table: "users", Columns: []string{"city", "balance"}}

	plain := f.run(t, f.config(false))
	if len(plain.rows) != 2 {
		t.Fatalf("unexpected baseline: %q", plain.rows)
	}

	first := f.run(t, f.config(true, index))
	if first.stats.CacheHit || !first.stats.CacheSaved {
		t.Fatalf("first run: hit=%v saved=%v", first.stats.CacheHit, first.stats.CacheSaved)
	}
	assertSameResult(t, first, plain)

	second := f.run(t, f.config(true, index))
	if !second.stats.CacheHit || second.stats.CacheSaved {
		t.Fatalf("second run: hit=%v saved=%v", second.stats.CacheHit, second.stats.CacheSaved)
	}
	assertSameResult(t, second, plain)
}

// Слишком большую базу Serialize не отдаёт: тогда она копируется через файл.
func TestWorkspaceCache_FileFallbackWhenSerializeUnavailable(t *testing.T) {
	original := serializeWorkspace
	t.Cleanup(func() { serializeWorkspace = original })
	called := false
	serializeWorkspace = func(*Workspace, context.Context) ([]byte, error) {
		called = true
		return nil, nil // так отвечает драйвер, когда SQLite не выделил образ
	}

	f := newCacheFixture(t)
	first := f.run(t, f.config(true))
	if !called || !first.stats.CacheSaved {
		t.Fatalf("called=%v saved=%v", called, first.stats.CacheSaved)
	}
	second := f.run(t, f.config(true))
	if !second.stats.CacheHit {
		t.Fatal("cache written through the fallback was not used")
	}
	assertSameResult(t, second, first)
}

func TestWorkspaceCache_RestoresIndexes(t *testing.T) {
	f := newCacheFixture(t)
	config := f.config(true, WorkspaceIndexConfig{Table: "users", Columns: []string{"city", "balance"}})
	f.run(t, config)

	ctx := context.Background()
	w, err := NewWorkspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close(ctx) }()
	key, err := workspaceCacheKey(config.Sources, config.Workspace.Cache.Indexes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.loadWorkspaceCache(ctx, f.cache, key); err != nil {
		t.Fatal(err)
	}

	var id, parent, unused int
	var detail string
	err = w.db.QueryRow("EXPLAIN QUERY PLAN SELECT * FROM users WHERE city = 'Omsk'").
		Scan(&id, &parent, &unused, &detail)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "tdtp_cache_ix_1") {
		t.Fatalf("restored workspace does not use the index: %s", detail)
	}
}

func TestWorkspaceCache_InvalidatedByInputs(t *testing.T) {
	f := newCacheFixture(t)
	f.run(t, f.config(true))

	// Другой список индексов — другая база.
	withIndex := f.run(t, f.config(true, WorkspaceIndexConfig{Table: "users", Columns: []string{"city"}}))
	if withIndex.stats.CacheHit || !withIndex.stats.CacheSaved {
		t.Fatalf("index change: hit=%v saved=%v", withIndex.stats.CacheHit, withIndex.stats.CacheSaved)
	}

	// Изменившийся источник того же размера и с тем же временем изменения:
	// сверяется содержимое, а не атрибуты файла.
	before, err := os.Stat(f.source)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.source)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(data), "300.25", "999.25", 1)
	if changed == string(data) {
		t.Fatal("source was not modified")
	}
	if err := os.WriteFile(f.source, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(f.source, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}

	after := f.run(t, f.config(true, WorkspaceIndexConfig{Table: "users", Columns: []string{"city"}}))
	if after.stats.CacheHit {
		t.Fatal("cache of the old source was used")
	}
	if !strings.Contains(strings.Join(after.rows[1], "|"), "999.25") {
		t.Fatalf("stale data: %q", after.rows)
	}
}

func TestWorkspaceCache_DamagedFileIsRebuilt(t *testing.T) {
	damage := map[string]func([]byte) []byte{
		"truncated payload": func(b []byte) []byte { return b[:len(b)-20] },
		"flipped payload":   func(b []byte) []byte { b[len(b)-30] ^= 0xFF; return b },
		"flipped header":    func(b []byte) []byte { b[len(workspaceCacheMagic)+4+16+2] ^= 0x01; return b },
		"magic only":        func(b []byte) []byte { return b[:len(workspaceCacheMagic)] },
	}
	for name, corrupt := range damage {
		t.Run(name, func(t *testing.T) {
			f := newCacheFixture(t)
			first := f.run(t, f.config(true))

			data, err := os.ReadFile(f.cache)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.cache, corrupt(data), 0o600); err != nil {
				t.Fatal(err)
			}

			rebuilt := f.run(t, f.config(true))
			if rebuilt.stats.CacheHit || !rebuilt.stats.CacheSaved {
				t.Fatalf("hit=%v saved=%v", rebuilt.stats.CacheHit, rebuilt.stats.CacheSaved)
			}
			assertSameResult(t, rebuilt, first)
		})
	}
}

func TestWorkspaceCache_DoesNotOverwriteForeignFile(t *testing.T) {
	f := newCacheFixture(t)
	original, err := os.ReadFile(f.source)
	if err != nil {
		t.Fatal(err)
	}

	config := f.config(true)
	config.Workspace.Cache.Path = f.source // опечатка: путь кеша указывает на источник
	result := f.run(t, config)

	if result.stats.CacheHit || result.stats.CacheSaved {
		t.Fatalf("hit=%v saved=%v", result.stats.CacheHit, result.stats.CacheSaved)
	}
	after, err := os.ReadFile(f.source)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("source file was overwritten")
	}
}

func TestWorkspaceCache_BadIndexFailsPipeline(t *testing.T) {
	f := newCacheFixture(t)
	config := f.config(true, WorkspaceIndexConfig{Table: "users", Columns: []string{"no_such_column"}})

	err := NewProcessor(config).Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), `no column "no_such_column"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(f.cache); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache must not be written: %v", err)
	}
}

func TestWorkspaceCache_ConfigValidation(t *testing.T) {
	f := newCacheFixture(t)
	cases := map[string]struct {
		change func(*PipelineConfig)
		want   string
	}{
		"no path":     {func(c *PipelineConfig) { c.Workspace.Cache.Path = "" }, "path is required"},
		"remote path": {func(c *PipelineConfig) { c.Workspace.Cache.Path = "s3://bucket/ws.zst" }, "local file"},
		"database source": {func(c *PipelineConfig) {
			c.Sources = append(c.Sources, SourceConfig{Name: "db", Type: "sqlite", DSN: "x.db", Query: "SELECT 1"})
		}, "only sources of type 'tdtp'"},
		"unknown table": {func(c *PipelineConfig) {
			c.Workspace.Cache.Indexes = []WorkspaceIndexConfig{{Table: "orders", Columns: []string{"id"}}}
		}, "not a source name"},
		"no columns": {func(c *PipelineConfig) {
			c.Workspace.Cache.Indexes = []WorkspaceIndexConfig{{Table: "users"}}
		}, "columns are required"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			config := f.config(true)
			tc.change(config)
			err := config.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}
