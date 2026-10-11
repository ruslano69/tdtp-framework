package commands

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/cliquery"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
	"github.com/ruslano69/tdtp-framework/pkg/tdtpjson"
	"github.com/ruslano69/tdtp-framework/pkg/xlsx"
)

func TestParallelCompact_MultipartRoundTrip(t *testing.T) {
	for _, mode := range []string{"plain", "tail", "zstd", "kanzi"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			sch := packet.Schema{Fields: []packet.Field{{Name: "id", Type: "INTEGER", Key: true}, {Name: "_dept", Type: "TEXT"}, {Name: "payload", Type: "TEXT"}}}
			rows := make([][]string, 12)
			for i := range rows {
				rows[i] = []string{strconv.Itoa(i + 1), "Sales", strings.Repeat("x", 1000)}
			}
			rows[1][1], rows[11][1] = "", ""
			g := packet.NewGenerator()
			g.SetMaxMessageSize(8192)
			parts, err := g.GenerateReference("emp", sch, rows)
			if err != nil {
				t.Fatal(err)
			}
			if len(parts) < 2 {
				t.Fatal("fixture must produce multiple parts")
			}
			total := len(parts)
			opts := ExportOptions{OutputFile: filepath.Join(t.TempDir(), "emp.xml"), Compact: true, FixedFields: []string{"_dept"}, CompactTail: mode == "tail", IntegrityV14: true}
			if mode == "zstd" || mode == "kanzi" {
				opts.Compress, opts.CompressAlgo, opts.CompressLevel = true, mode, 3
			}
			chain, err := buildExportChain(sch, opts)
			if err != nil {
				t.Fatal(err)
			}
			warnings := captureStderr(t, func() {
				if err := parallelProcessAndWrite(ctx, parts, chain, total, opts, nil); err != nil {
					t.Error(err)
				}
			})
			if t.Failed() {
				return
			}
			if n := strings.Count(warnings, "compact: dept is not fixed"); n != 1 {
				t.Fatalf("warnings=%d, want 1", n)
			}
			first := generatePacketFilename(opts.OutputFile, 1, total)
			dst := filepath.Join(t.TempDir(), "dst.db")
			cfg := adapters.Config{Type: "sqlite", DSN: dst}
			if err := ImportFile(ctx, &cfg, ImportOptions{FilePath: first, Strategy: adapters.StrategyReplace}); err != nil {
				t.Fatal(err)
			}
			a, err := adapters.New(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close(ctx)
			got, err := a.(interface {
				ExecuteRawQuery(context.Context, string) (*packet.DataPacket, error)
			}).ExecuteRawQuery(ctx, "SELECT id, dept, payload FROM emp ORDER BY id")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.GetRows(), rows) {
				t.Fatal("parallel compact export/import changed source cells")
			}
		})
	}
}

func TestNullLiteral_TextConversions(t *testing.T) {
	for _, hasNull := range []bool{false, true} {
		for _, mode := range []string{"plain", "compact", "zstd", "kanzi"} {
			t.Run(fmt.Sprintf("%s/null=%v", mode, hasNull), func(t *testing.T) {
				ctx := context.Background()
				rows := [][]string{{"1", "Sales", "[NULL]"}, {"2", "Sales", "[NULL1]"}, {"3", "Sales", "[NULL2]"}, {"4", "Sales", ""}}
				if hasNull {
					rows = append(rows, []string{"5", "Sales", "\x00"})
				}
				sch := packet.Schema{Fields: []packet.Field{{Name: "id", Type: "INTEGER", Key: true}, {Name: "dept", Type: "TEXT"}, {Name: "note", Type: "TEXT"}}}
				g := packet.NewGenerator()
				parts, err := g.GenerateReference("emp", sch, rows)
				if err != nil {
					t.Fatal(err)
				}
				p := parts[0]
				opts := ExportOptions{Compact: mode == "compact", FixedFields: []string{"dept"}, IntegrityV14: true}
				if mode == "zstd" || mode == "kanzi" {
					opts.Compress, opts.CompressAlgo, opts.CompressLevel = true, mode, 3
				}
				chain, err := buildExportChain(sch, opts)
				if err != nil {
					t.Fatal(err)
				}
				if err := chain.ProcessPacket(ctx, p); err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(t.TempDir(), "emp.xml")
				if err := g.WriteToFile(p, file); err != nil {
					t.Fatal(err)
				}
				read := func() *packet.DataPacket {
					t.Helper()
					b, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					q, err := packet.NewParser().ParseBytes(b)
					if err != nil {
						t.Fatal(err)
					}
					if err := processors.DecompressPacket(ctx, q); err != nil {
						t.Fatal(err)
					}
					if err := packet.VerifyIntegrity(q); err != nil {
						t.Fatal(err)
					}
					if err := packet.ExpandCompactRows(q); err != nil {
						t.Fatal(err)
					}
					return q
				}
				p = read()
				var b bytes.Buffer
				if _, err := tdtpjson.Write(ctx, &b, p, nil, false); err != nil {
					t.Fatal(err)
				}
				var objects []map[string]any
				if err := json.Unmarshal(b.Bytes(), &objects); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 4; i++ {
					if objects[i]["note"] != rows[i][2] {
						t.Fatalf("JSON row %d note=%v, want %q", i, objects[i]["note"], rows[i][2])
					}
				}
				if hasNull && objects[4]["note"] != nil {
					t.Fatal("SQL NULL became text")
				}
				for where, want := range map[string]int{"note IS NULL": 0, "note IS NOT NULL": 4} {
					if hasNull && where == "note IS NULL" {
						want = 1
					}
					q, err := cliquery.BuildQuery([]string{where}, "", 0, 0)
					if err != nil {
						t.Fatal(err)
					}
					b.Reset()
					n, err := tdtpjson.Write(ctx, &b, read(), q, false)
					if err != nil {
						t.Fatal(err)
					}
					if n != want {
						t.Fatalf("%s matched %d rows, want %d", where, n, want)
					}
				}
				csvFile := filepath.Join(t.TempDir(), "emp.csv")
				if err := ConvertTDTPToCSV(ctx, CSVOptions{InputFile: file, OutputFile: csvFile}); err != nil {
					t.Fatal(err)
				}
				csvBytes, err := os.ReadFile(csvFile)
				if err != nil {
					t.Fatal(err)
				}
				records, err := csv.NewReader(bytes.NewReader(csvBytes)).ReadAll()
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 4; i++ {
					if records[i+1][2] != rows[i][2] {
						t.Fatal("CSV lost literal text")
					}
				}
				xlsxFile := filepath.Join(t.TempDir(), "emp.xlsx")
				if err := xlsx.ToXLSX(read(), xlsxFile, "emp"); err != nil {
					t.Fatal(err)
				}
				back, err := xlsx.FromXLSX(xlsxFile, "emp")
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 4; i++ {
					if back.GetRows()[i][2] != rows[i][2] {
						t.Fatal("XLSX lost literal text")
					}
				}
				cfg := adapters.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "literal.db")}
				if err := ImportFile(ctx, &cfg, ImportOptions{FilePath: file, Strategy: adapters.StrategyReplace}); err != nil {
					t.Fatal(err)
				}
				a, err := adapters.New(ctx, cfg)
				if err != nil {
					t.Fatal(err)
				}
				cells := readCells(ctx, t, a, "emp")
				_ = a.Close(ctx)
				for i := 0; i < 4; i++ {
					if cells[rows[i][0]][2] != rows[i][2] {
						t.Fatal("SQLite import lost literal text")
					}
				}
				if hasNull && cells["5"][2] != nil {
					t.Fatal("SQLite import lost NULL")
				}

				// Re-generation must keep the marker and its NULL meaning.
				p = read()
				regen, err := g.GenerateReference("emp", p.Schema, p.GetRows())
				if err != nil {
					t.Fatal(err)
				}
				if packet.NullMarkerOf(regen[0].Schema.Fields[2]) != packet.NullMarkerOf(p.Schema.Fields[2]) {
					t.Fatal("regeneration changed the marker")
				}
			})
		}
	}
}
