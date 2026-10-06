package adapters_test

// DECIMAL must travel exactly through every engine. The import path used to
// parse DECIMAL into float64 (~16 significant digits), so money values were
// rounded silently on any cross-engine transfer; PostgreSQL and MySQL also
// created DECIMAL(p,0) as NUMERIC(p,2), which cannot hold a p-digit integer.
// Runs against every engine that is reachable (SQLite always); set
// POSTGRES_TEST_DSN, MYSQL_TEST_DSN, MSSQL_TEST_DSN_PROD, TDTP_ORACLE21_DSN.

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mysql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/oracle"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func liveEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestDecimalExactAcrossEngines(t *testing.T) {
	engines := map[string]adapters.Config{
		"sqlite": {Type: "sqlite", DSN: filepath.Join(t.TempDir(), "d.db")},
		"postgres": {Type: "postgres", DSN: liveEnv("POSTGRES_TEST_DSN",
			"postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test")},
		"mysql": {Type: "mysql", DSN: liveEnv("MYSQL_TEST_DSN",
			"tdtp_user:tdtp_dev_pass_2025@tcp(127.0.0.1:3306)/tdtp_test?parseTime=true")},
		"mssql": {Type: "mssql", DSN: liveEnv("MSSQL_TEST_DSN_PROD",
			"server=localhost,1434;user id=sa;password=ProdPassword123!;database=ProdSimDB;encrypt=disable")},
	}
	if dsn := os.Getenv("TDTP_ORACLE21_DSN"); dsn != "" {
		engines["oracle"] = adapters.Config{Type: "oracle", DSN: dsn}
	}
	schema := packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "money", Type: "DECIMAL", Precision: 20, Scale: 4},
		{Name: "wide", Type: "DECIMAL", Precision: 38, Scale: 10},
		{Name: "whole", Type: "DECIMAL", Precision: 19, Scale: 0},
		{Name: "cents", Type: "DECIMAL", Precision: 10, Scale: 2},
	}}
	rows := [][]string{
		{"1", "1234567890123450.1234", "1234567890123456789012345678.0123456789", "1234567890123456789", "0.10"},
		{"2", "-0.0001", "-0.0000000001", "-9007199254740993", "-12345678.99"},
	}
	for name, cfg := range engines {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			a, err := adapters.New(ctx, cfg)
			if err != nil {
				t.Skipf("%s not reachable: %v", name, err)
			}
			defer a.Close(ctx)
			table := fmt.Sprintf("tdtp_dec_%d", time.Now().UnixNano()%1_000_000_000)
			pkts, err := packet.NewGenerator().GenerateReference(table, schema, rows)
			if err != nil {
				t.Fatal(err)
			}
			if d, ok := a.(interface {
				DropTable(context.Context, string) error
			}); ok {
				defer func() { _ = d.DropTable(context.Background(), table) }()
			}
			if err := a.ImportPacket(ctx, pkts[0], adapters.StrategyReplace); err != nil {
				t.Fatalf("import: %v", err)
			}
			back, err := a.ExportTable(ctx, table)
			if err != nil {
				t.Fatalf("export: %v", err)
			}
			// The exported schema must declare what was created. A wrong
			// (p,s) is not visible in the values: a cell that fails to parse
			// is passed through raw, so MySQL's (18,2) for every DECIMAL and
			// MSSQL's 18 for decimal(19,0) went unnoticed.
			for i, f := range back[0].Schema.Fields {
				w := schema.Fields[i]
				if w.Type == "DECIMAL" && (f.Precision != w.Precision || f.Scale != w.Scale) {
					t.Errorf("%s.%s exported as (%d,%d), created as (%d,%d)",
						name, f.Name, f.Precision, f.Scale, w.Precision, w.Scale)
				}
			}
			got := map[string][]string{}
			for _, r := range back[0].GetRows() {
				got[r[0]] = r
			}
			for _, want := range rows {
				g := got[want[0]]
				for c := 1; c < len(want); c++ {
					v := "<missing>"
					if c < len(g) {
						v = g[c]
					}
					if name == "sqlite" && !sameDecimal(v, want[c]) && beyondDouble(want[c]) {
						// SQLite has no decimal storage: NUMERIC affinity keeps a
						// non-integer as REAL, 15–17 significant digits. Pin that it
						// is no worse than a double, and that it is only these cells.
						if !sameAsDouble(v, want[c]) {
							t.Errorf("sqlite.%s row %s: sent %s, got %s — worse than float64",
								schema.Fields[c].Name, want[0], want[c], v)
						}
						continue
					}
					if !sameDecimal(v, want[c]) {
						t.Errorf("%s.%s row %s: sent %s, got %s", name, schema.Fields[c].Name, want[0], want[c], v)
					}
				}
			}
		})
	}
}

// sameDecimal compares as exact rationals: "0.10" == "0.1", never via float.
func sameDecimal(a, b string) bool {
	x, ok1 := new(big.Rat).SetString(a)
	y, ok2 := new(big.Rat).SetString(b)
	return ok1 && ok2 && x.Cmp(y) == 0
}

// beyondDouble reports a decimal float64 cannot hold exactly.
func beyondDouble(s string) bool {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return false
	}
	_, exact := r.Float64()
	return !exact
}

// sameAsDouble: both sides round to the same float64.
func sameAsDouble(a, b string) bool {
	x, ok1 := new(big.Rat).SetString(a)
	y, ok2 := new(big.Rat).SetString(b)
	if !ok1 || !ok2 {
		return false
	}
	fx, _ := x.Float64()
	fy, _ := y.Float64()
	return fx == fy
}
