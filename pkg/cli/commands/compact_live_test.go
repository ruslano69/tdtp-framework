package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mysql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/oracle"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func liveDSN(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// The compact/NULL-marker round trip of TestCompactRoundTrip_EmptyAndNullLiterals
// against live servers: the table is created by our import, exported with
// --compact --compress --integrity, imported into a second table, and both are
// read back. Skips an engine it cannot reach.
func TestCompactRoundTrip_LiveEngines(t *testing.T) {
	engines := map[string]adapters.Config{
		"postgres": {Type: "postgres", DSN: liveDSN("POSTGRES_TEST_DSN",
			"postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test")},
		"mysql": {Type: "mysql", DSN: liveDSN("MYSQL_TEST_DSN",
			"tdtp_user:tdtp_dev_pass_2025@tcp(127.0.0.1:3306)/tdtp_test?parseTime=true")},
		"mssql": {Type: "mssql", DSN: liveDSN("MSSQL_TEST_DSN_PROD",
			"server=localhost,1434;user id=sa;password=ProdPassword123!;database=ProdSimDB;encrypt=disable")},
	}
	if dsn := os.Getenv("TDTP_ORACLE21_DSN"); dsn != "" {
		engines["oracle"] = adapters.Config{Type: "oracle", DSN: dsn}
	}
	const null = "\x00" // the adapters' NULL sentinel, as a DB read yields it
	schema := packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "dept", Type: "TEXT"}, {Name: "city", Type: "TEXT"},
		{Name: "name", Type: "TEXT"}, {Name: "note", Type: "TEXT"},
	}}
	rows := [][]string{
		{"1", "Sales", "Moscow", "Ivan", "a"},
		{"2", "Sales", "Moscow", "Anna", ""},
		{"3", "", "Moscow", "Boris", "b"},
		{"4", "", "Moscow", "Elena", null},
		{"5", "Sales", "Moscow", "Dmitry", "c"},
		{"6", null, "Kazan", "Oleg", "d"},
		{"7", null, "Kazan", "Petr", "[NULL]"},
		{"8", "[NULL]", "Kazan", "Rita", "e"},
		{"9", "IT", "", "Sergey", "f"},
	}

	for name, cfg := range engines {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			a, err := adapters.New(ctx, cfg)
			if err != nil {
				t.Skipf("%s not reachable: %v", name, err)
			}
			defer a.Close(ctx)
			n := time.Now().UnixNano() % 1_000_000_000
			src, dst := fmt.Sprintf("tdtp_cmp_src_%d", n), fmt.Sprintf("tdtp_cmp_dst_%d", n)
			if d, ok := a.(interface {
				DropTable(context.Context, string) error
			}); ok {
				defer func() { _ = d.DropTable(context.Background(), src); _ = d.DropTable(context.Background(), dst) }()
			}

			seed, err := packet.NewGenerator().GenerateReference(src, schema, rows)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.ImportPacket(ctx, seed[0], adapters.StrategyReplace); err != nil {
				t.Fatalf("seed: %v", err)
			}

			file := filepath.Join(t.TempDir(), "cmp.tdtp.xml")
			if err := ExportTable(ctx, &cfg, ExportOptions{TableName: src, OutputFile: file,
				Compact: true, FixedFields: []string{"dept", "city"}, Compress: true, IntegrityV14: true}); err != nil {
				t.Fatalf("export: %v", err)
			}
			if err := ImportFile(ctx, &cfg, ImportOptions{FilePath: file, TargetTable: dst,
				Strategy: adapters.StrategyReplace}); err != nil {
				t.Fatalf("import: %v", err)
			}

			want, got := readCells(ctx, t, a, src), readCells(ctx, t, a, dst)
			if !reflect.DeepEqual(got, want) {
				for id, w := range want {
					if !reflect.DeepEqual(got[id], w) {
						t.Errorf("%s row %s:\n got %q\nwant %q", name, id, got[id], w)
					}
				}
			}
			// The source must hold what was seeded — otherwise src == dst
			// could be two copies of the same loss. Oracle stores '' as NULL.
			for _, r := range rows {
				for c := 1; c < len(r); c++ {
					var w any = r[c]
					if r[c] == null || (name == "oracle" && r[c] == "") {
						w = nil
					}
					if g := want[r[0]][c]; !reflect.DeepEqual(g, w) {
						t.Errorf("%s source row %s col %d holds %q, seeded %q", name, r[0], c, g, w)
					}
				}
			}
		})
	}
}

// readCells exports a table and decodes each field's NULL marker to nil.
func readCells(ctx context.Context, t *testing.T, a adapters.Adapter, table string) map[string][]any {
	t.Helper()
	pkts, err := a.ExportTable(ctx, table)
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	out := map[string][]any{}
	for _, p := range pkts {
		for _, r := range p.GetRows() {
			cells := make([]any, len(r))
			for i, v := range r {
				if v == packet.NullMarkerOf(p.Schema.Fields[i]) && p.Schema.Fields[i].SpecialValues != nil {
					cells[i] = nil
				} else {
					cells[i] = v
				}
			}
			out[r[0]] = cells
		}
	}
	return out
}
