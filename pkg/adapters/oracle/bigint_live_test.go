package oracle

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mysql"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// INTEGER → Oracle NUMBER(19,0) → DECIMAL+"bigint" → BIGINT elsewhere, with
// 2^53+1 exact the whole way (float64 would round it to 2^53). And a native
// NUMBER(19,0) value above int64 must make the BIGINT target refuse, never
// arrive rounded. Runs per target that is reachable; skips otherwise.
func TestOracleLiveBigintToOtherEngines(t *testing.T) {
	dsn := os.Getenv("TDTP_ORACLE21_DSN")
	if dsn == "" {
		dsn = os.Getenv("TDTP_ORACLE18_DSN")
	}
	if dsn == "" {
		t.Skip("Oracle test DSN is not configured")
	}
	targets := map[string]adapters.Config{
		"mssql": {Type: "mssql", DSN: envOr("MSSQL_TEST_DSN_PROD",
			"server=localhost,1434;user id=sa;password=ProdPassword123!;database=ProdSimDB;encrypt=disable")},
		"mysql": {Type: "mysql", DSN: envOr("MYSQL_TEST_DSN",
			"tdtp_user:tdtp_dev_pass_2025@tcp(127.0.0.1:3306)/tdtp_test?parseTime=true")},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	g, err := adapters.New(ctx, adapters.Config{Type: AdapterType, DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	ora := g.(*Adapter)
	t.Cleanup(func() { _ = ora.Close(context.Background()) })

	unique := time.Now().UnixNano() % 1_000_000_000
	src := fmt.Sprintf("tdtp_bi_%d", unique)
	t.Cleanup(func() { _ = ora.DropTable(context.Background(), src) })
	pkts, err := packet.NewGenerator().GenerateReference(src, packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "n", Type: "INTEGER"},
	}}, [][]string{{"1", "9007199254740993"}, {"2", "-9223372036854775808"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ora.ImportPacket(ctx, pkts[0], adapters.StrategyReplace); err != nil {
		t.Fatalf("INTEGER into Oracle: %v", err)
	}
	back, err := ora.ExportTable(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range back[0].Schema.Fields {
		if !packet.BigintDecimal(f) {
			t.Fatalf("Oracle reported %s as %s/%q, want DECIMAL/bigint", f.Name, f.Type, f.Subtype)
		}
	}

	// A native NUMBER(19,0) holding more than int64.
	huge := fmt.Sprintf("tdtp_bh_%d", unique)
	t.Cleanup(func() { _ = ora.DropTable(context.Background(), huge) })
	if _, err := ora.db.ExecContext(ctx, `CREATE TABLE `+quote(strings.ToUpper(huge))+` (ID NUMBER(19,0) PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := ora.db.ExecContext(ctx, `INSERT INTO `+quote(strings.ToUpper(huge))+` VALUES (9500000000000000000)`); err != nil {
		t.Fatal(err)
	}
	hugeBack, err := ora.ExportTable(ctx, huge)
	if err != nil {
		t.Fatal(err)
	}

	for name, cfg := range targets {
		t.Run(name, func(t *testing.T) {
			dst, err := adapters.New(ctx, cfg)
			if err != nil {
				t.Skipf("%s not reachable: %v", name, err)
			}
			defer dst.Close(ctx)
			dropper, _ := dst.(interface {
				DropTable(context.Context, string) error
			})

			p := *back[0]
			p.Header.TableName = src
			if err := dst.ImportPacket(ctx, &p, adapters.StrategyReplace); err != nil {
				t.Fatalf("import: %v", err)
			}
			if dropper != nil {
				defer func() { _ = dropper.DropTable(ctx, src) }()
			}
			got, err := dst.ExportTable(ctx, src)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range got[0].Schema.Fields {
				if f.Type != "INTEGER" {
					t.Errorf("%s: column %s came back %s/%q, want an INTEGER (BIGINT) column", name, f.Name, f.Type, f.Subtype)
				}
			}
			rows := map[string]string{}
			for _, r := range got[0].GetRows() {
				rows[r[0]] = r[1]
			}
			if rows["1"] != "9007199254740993" || rows["2"] != "-9223372036854775808" {
				t.Errorf("%s: values %v, want 2^53+1 and min int64 exactly", name, rows)
			}

			h := *hugeBack[0]
			h.Header.TableName = huge
			err = dst.ImportPacket(ctx, &h, adapters.StrategyReplace)
			if dropper != nil {
				defer func() { _ = dropper.DropTable(ctx, huge) }()
			}
			if err == nil {
				t.Errorf("%s: 9500000000000000000 into BIGINT was accepted; it must be refused, not rounded", name)
			}
		})
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
