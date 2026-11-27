package etl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func partFields() []packet.Field {
	return []packet.Field{
		{Name: "Id", Type: "INTEGER", Key: true},
		{Name: "City", Type: "TEXT", Length: 60},
		{Name: "Total %", Type: "DECIMAL", Precision: 12, Scale: 2},
		{Name: "Born", Type: "DATE"},
	}
}

func partRows(from, count int) [][]string {
	cities := []string{"Omsk", "", "Ufa", "A|B", `C\D`, "[NULL]"}
	rows := make([][]string, count)
	for i := range rows {
		id := from + i
		rows[i] = []string{strconv.Itoa(id), cities[id%len(cities)], fmt.Sprintf("%d.%02d", id, id%100), "1990-05-15"}
	}
	return rows
}

type testPart struct {
	fields []packet.Field
	rows   [][]string
}

// writeParts пишет набор users_part_N_of_T.xml и возвращает путь первой части.
func writeParts(t *testing.T, dir, base string, parts ...testPart) string {
	t.Helper()
	var first string
	for i, part := range parts {
		pkt := packet.NewDataPacket(packet.TypeReference, "users")
		pkt.Schema = packet.Schema{Fields: slices.Clone(part.fields)}
		pkt.Data = packet.RowsToData(part.rows)
		pkt.Header.RecordsInPart = len(part.rows)
		pkt.Header.PartNumber, pkt.Header.TotalParts = i+1, len(parts)
		path := filepath.Join(dir, fmt.Sprintf("%s_part_%d_of_%d.xml", base, i+1, len(parts)))
		if err := packet.NewGenerator().WriteToFile(pkt, path); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = path
		}
	}
	return first
}

func threeParts() []testPart {
	return []testPart{
		{partFields(), partRows(1, 40)},
		{partFields(), partRows(41, 25)},
		{partFields(), partRows(66, 7)},
	}
}

func partsProcessor(t *testing.T, sources ...SourceConfig) *Processor {
	t.Helper()
	p := NewProcessor(&PipelineConfig{Sources: sources})
	w, err := NewWorkspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p.workspace = w
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	return p
}

func tableRows(t *testing.T, w *Workspace, query string) (packet.Schema, [][]string) {
	t.Helper()
	pkt, err := w.ExecuteSQL(context.Background(), query, "r")
	if err != nil {
		t.Fatal(err)
	}
	return pkt.Schema, pkt.GetRows()
}

// Эталон — прежний путь: склеить все части в один пакет и вставить разом.
func TestLoadByPart_SameTableAsMergedPacket(t *testing.T) {
	for _, sanitized := range []bool{false, true} {
		t.Run(fmt.Sprintf("sanitize=%v", sanitized), func(t *testing.T) {
			source := SourceConfig{Name: "users", Type: "tdtp", MultiPart: true,
				DSN: writeParts(t, t.TempDir(), "users", threeParts()...)}
			if sanitized {
				source.Sanitize = &SanitizeFieldsConfig{Clear: true}
			}

			reference := partsProcessor(t, source)
			merged, err := loadTDTPFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := reference.populateWorkspace(context.Background(),
				[]SourceData{{SourceName: "users", TableName: "users", Packet: merged}}); err != nil {
				t.Fatal(err)
			}

			p := partsProcessor(t, source)
			data, err := p.loadAndPopulate(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			wantSchema, wantRows := tableRows(t, reference.workspace, "SELECT * FROM users ORDER BY rowid")
			gotSchema, gotRows := tableRows(t, p.workspace, "SELECT * FROM users ORDER BY rowid")
			if len(wantRows) != 72 {
				t.Fatalf("unexpected reference: %d rows", len(wantRows))
			}
			if !reflect.DeepEqual(gotRows, wantRows) || !reflect.DeepEqual(gotSchema, wantSchema) {
				t.Fatal("table differs from the merged-packet load")
			}

			if len(data) != 1 || data[0].Packet.Header.RecordsInPart != 72 || len(data[0].Packet.Data.Rows) != 0 {
				t.Fatalf("want a rowless packet counting 72 rows, got %+v", data)
			}
			if !reflect.DeepEqual(data[0].Packet.Schema, merged.Schema) {
				t.Fatalf("schema for passthrough differs:\n got %+v\nwant %+v", data[0].Packet.Schema, merged.Schema)
			}
			if p.stats.SourcesLoaded != 1 || p.stats.TotalRowsLoaded != 72 {
				t.Fatalf("stats: %d sources, %d rows", p.stats.SourcesLoaded, p.stats.TotalRowsLoaded)
			}
		})
	}
}

// Каждая compact-часть разворачивается по своей схеме: fixed-флаги у частей
// могут различаться.
func TestLoadByPart_CompactParts(t *testing.T) {
	fixed := func(names ...string) []packet.Field {
		fields := partFields()
		for i := range fields {
			fields[i].Fixed = slices.Contains(names, fields[i].Name)
		}
		return fields
	}
	dir := t.TempDir()
	var first string
	for i, part := range []testPart{
		{fixed("City"), [][]string{{"1", "Omsk", "1.00", "1990-05-15"}, {"2", "", "2.00", "1990-05-15"}}},
		{fixed("Born"), [][]string{{"3", "Ufa", "3.00", "2000-01-01"}, {"4", "Perm", "4.00", ""}}},
	} {
		pkt := packet.NewDataPacket(packet.TypeReference, "users")
		pkt.Schema = packet.Schema{Fields: part.fields}
		pkt.Data = packet.RowsToData(part.rows)
		pkt.Data.Compact = true
		pkt.Header.RecordsInPart = len(part.rows)
		path := filepath.Join(dir, fmt.Sprintf("c_part_%d_of_2.xml", i+1))
		if err := packet.NewGenerator().WriteToFile(pkt, path); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = path
		}
	}

	p := partsProcessor(t, SourceConfig{Name: "users", Type: "tdtp", MultiPart: true, DSN: first})
	if _, err := p.loadAndPopulate(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, rows := tableRows(t, p.workspace, `SELECT Id, City, Born FROM users ORDER BY Id`)
	got := make([]string, len(rows))
	for i, row := range rows {
		got[i] = strings.Join(row, ";")
	}
	want := []string{"1;Omsk;1990-05-15", "2;Omsk;1990-05-15", "3;Ufa;2000-01-01", "4;Perm;2000-01-01"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Всё или ничего: плохая часть в середине обрывает пайплайн, результата нет.
func TestLoadByPart_BadPartFailsPipeline(t *testing.T) {
	damage := map[string]func(t *testing.T, dir, second string){
		// Колонки того же числа, но другая: по позициям вставилась бы молча.
		"other schema": func(t *testing.T, dir, _ string) {
			parts := threeParts()
			parts[1].fields[1].Name = "Town"
			writeParts(t, dir, "users", parts...)
		},
		"truncated": func(t *testing.T, _, second string) {
			data, err := os.ReadFile(second)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(second, data[:len(data)/2], 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"missing": func(t *testing.T, _, second string) {
			if err := os.Remove(second); err != nil {
				t.Fatal(err)
			}
		},
		"short row": func(t *testing.T, _, second string) {
			data, err := os.ReadFile(second)
			if err != nil {
				t.Fatal(err)
			}
			changed := strings.Replace(string(data), "|1990-05-15</R>", "</R>", 1)
			if err := os.WriteFile(second, []byte(changed), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, corrupt := range damage {
		t.Run(name, func(t *testing.T) {
			// Свой каталог на случай: записанный одним результат не должен
			// быть виден следующему.
			f := newCacheFixture(t)
			first := writeParts(t, f.dir, "users", threeParts()...)
			corrupt(t, f.dir, strings.Replace(first, "_part_1_", "_part_2_", 1))
			for _, cached := range []bool{false, true} {
				config := f.config(cached)
				config.Sources[0].DSN, config.Sources[0].MultiPart = first, true
				err := NewProcessor(config).Execute(context.Background())
				if err == nil || !strings.Contains(err.Error(), "users_part_2_of_3") &&
					!strings.Contains(err.Error(), "row ") {
					t.Fatalf("cached=%v: unexpected error: %v", cached, err)
				}
				for _, path := range []string{f.output, f.cache} {
					if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
						t.Fatalf("cached=%v: %s must not exist", cached, path)
					}
				}
			}
		})
	}
}

func TestLoadByPart_MultipartWithCache(t *testing.T) {
	f := newCacheFixture(t)
	first := writeParts(t, f.dir, "users", threeParts()...)
	config := func(cached bool) *PipelineConfig {
		c := f.config(cached)
		c.Sources[0].DSN, c.Sources[0].MultiPart = first, true
		return c
	}

	plain := f.run(t, config(false))
	built := f.run(t, config(true))
	hit := f.run(t, config(true))
	if !built.stats.CacheSaved || !hit.stats.CacheHit {
		t.Fatalf("saved=%v hit=%v", built.stats.CacheSaved, hit.stats.CacheHit)
	}
	assertSameResult(t, built, plain)
	assertSameResult(t, hit, plain)
	if plain.stats.TotalRowsLoaded != 72 {
		t.Fatalf("rows loaded: %d", plain.stats.TotalRowsLoaded)
	}
}

func TestLoadByPart_MixedWithOtherSources(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "cities.csv")
	if err := os.WriteFile(csvPath, []byte("name,region\nOmsk,55\nUfa,02\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := partsProcessor(t,
		SourceConfig{Name: "users", Type: "tdtp", MultiPart: true, DSN: writeParts(t, dir, "users", threeParts()...)},
		SourceConfig{Name: "cities", Type: "csv", DSN: csvPath, CSV: &CSVSourceConfig{Columns: []string{"name", "region"}}},
		SourceConfig{Name: "more", Type: "tdtp", DSN: writeParts(t, dir, "more", testPart{partFields(), partRows(1, 5)})},
	)
	data, err := p.loadAndPopulate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 3 || p.stats.SourcesLoaded != 3 || p.stats.TotalRowsLoaded != 72+2+5 {
		t.Fatalf("data=%d sources=%d rows=%d", len(data), p.stats.SourcesLoaded, p.stats.TotalRowsLoaded)
	}
	_, rows := tableRows(t, p.workspace, "SELECT COUNT(*) FROM users u JOIN cities c ON c.name = u.City")
	if rows[0][0] == "0" {
		t.Fatal("join across sources returned nothing")
	}
}

func TestLoadByPart_CancelledContext(t *testing.T) {
	p := partsProcessor(t, SourceConfig{Name: "users", Type: "tdtp", MultiPart: true,
		DSN: writeParts(t, t.TempDir(), "users", threeParts()...)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.loadByPart(ctx, p.config.Sources[0]); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestCheckPartSchema(t *testing.T) {
	marker := func(m string) *packet.SpecialValues {
		return &packet.SpecialValues{Null: &packet.MarkerValue{Marker: m}}
	}
	infinity := &packet.SpecialValues{Infinity: &packet.MarkerValue{Marker: "INF"}}

	cases := map[string]struct {
		change      func(first, part []packet.Field)
		city, total string // значения в строке проверяемой части
		want        string // "" — часть принимается
	}{
		"same":                  {change: func(_, _ []packet.Field) {}},
		"length may differ":     {change: func(_, part []packet.Field) { part[1].Length = 500 }},
		"renamed field":         {change: func(_, part []packet.Field) { part[1].Name = "Town" }, want: "field 2"},
		"swapped fields":        {change: func(_, part []packet.Field) { part[0], part[1] = part[1], part[0] }, want: "field 1"},
		"other type":            {change: func(_, part []packet.Field) { part[2].Type = "REAL" }, want: "field 3"},
		"other subtype":         {change: func(_, part []packet.Field) { part[1].Subtype = "uuid" }, want: "field 2"},
		"declared legacy equal": {change: func(_, part []packet.Field) { part[1].SpecialValues = marker("[NULL]") }, city: "[NULL]"},
		"markers differ, unused": {change: func(_, part []packet.Field) { part[1].SpecialValues = marker("[NULL1]") },
			city: "Omsk"},
		// "[NULL1]" здесь означает NULL, а по схеме первой части — текст.
		"own marker used": {change: func(_, part []packet.Field) { part[1].SpecialValues = marker("[NULL1]") },
			city: "[NULL1]", want: "markers differ"},
		// Здесь "[NULL]" — текст, а по схеме первой части — NULL.
		"first marker as text": {change: func(_, part []packet.Field) { part[1].SpecialValues = marker("[NULL1]") },
			city: "[NULL]", want: "markers differ"},
		"first declares its own": {change: func(first, _ []packet.Field) { first[1].SpecialValues = marker("[NULL1]") },
			city: "[NULL]", want: "markers differ"},
		"infinity only here": {change: func(_, part []packet.Field) { part[2].SpecialValues = infinity },
			total: "INF", want: "markers differ"},
		"infinity declared, absent": {change: func(_, part []packet.Field) { part[2].SpecialValues = infinity }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			first, fields := partFields(), partFields()
			tc.change(first, fields)
			row := []string{"1", "Omsk", "1.00", "1990-05-15"}
			if tc.city != "" {
				row[1] = tc.city
			}
			if tc.total != "" {
				row[2] = tc.total
			}
			pkt := &packet.DataPacket{Schema: packet.Schema{Fields: fields}, Data: packet.RowsToData([][]string{row})}

			err := checkPartSchema(first, pkt)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}

	short := &packet.DataPacket{Schema: packet.Schema{Fields: partFields()[:3]}}
	if err := checkPartSchema(partFields(), short); err == nil {
		t.Fatal("field count mismatch must fail")
	}
}
