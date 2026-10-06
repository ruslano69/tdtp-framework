package oracle

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/cliquery"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// A packet from another engine, imported and read back on 18c and 21c.
// The contract test above uses an Oracle-made table (upper-case columns,
// sized types); this one is what SQLite or PostgreSQL hand over: lower-case
// names, a TEXT key without length, a 64-bit REAL. Three defects lived only
// on this path:
//   - every --where/--order-by failed with ORA-00904 (bare `name` → NAME)
//     and fell back to reading the whole table into memory;
//   - the TEXT key became a CLOB key: ORA-02329, import impossible;
//   - REAL became BINARY_FLOAT: 1234.56789012345 came back 1234.5679.
func TestOracleLiveCrossEngine(t *testing.T) {
	for _, version := range []string{"18", "21"} {
		dsn := os.Getenv("TDTP_ORACLE" + version + "_DSN")
		if dsn == "" {
			t.Run(version+"c", func(t *testing.T) { t.Skip("Oracle test DSN is not configured") })
			continue
		}
		t.Run(version+"c", func(t *testing.T) { crossEngine(t, dsn) })
	}
}

func crossEngine(t *testing.T, dsn string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	g, err := adapters.New(ctx, adapters.Config{Type: AdapterType, DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	a := g.(*Adapter)
	t.Cleanup(func() { _ = a.Close(context.Background()) })

	table := fmt.Sprintf("tdtp_xe_%d", time.Now().UnixNano()%1_000_000_000)
	t.Cleanup(func() { _ = a.DropTable(context.Background(), table) })
	schema := packet.Schema{Fields: []packet.Field{
		{Name: "code", Type: "TEXT", Key: true},
		{Name: "name", Type: "TEXT"},
		{Name: "score", Type: "REAL"},
		{Name: "born", Type: "DATE"},
		{Name: "seen", Type: "DATETIME"},
		{Name: "stamp", Type: "TIMESTAMP"},
	}}
	pkts, err := packet.NewGenerator().GenerateReference(table, schema, [][]string{
		{"A1", "Иван Петров", "1234.56789012345", "1990-05-17", "2026-01-02 03:04:05", "2026-01-02T03:04:05Z"},
		{"B2", "Olga", "0.1", "2001-02-03", "2026-03-04 05:06:07", "2026-03-04T05:06:07Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ImportPacket(ctx, pkts[0], adapters.StrategyReplace); err != nil {
		t.Fatalf("import a lengthless TEXT key: %v", err)
	}

	// A fallback must not be able to hide behind a correct answer: with a
	// limit of one row, a failed pushdown aborts instead of scanning.
	a.SetMaxFallbackRows(1)
	for _, c := range []struct {
		where, order, want string
	}{
		{"name LIKE 'Ив%'", "", "A1"},
		{"NAME LIKE 'Ol%'", "", "B2"}, // case-insensitive column lookup
		{"score > 1000", "", "A1"},
		{"", "code DESC", "B2,A1"},
		// Dates compared with plain strings failed with ORA-01861 (NLS
		// DD-MON-RR) and fell back; ANSI literals do not depend on NLS.
		{"born > '1995-01-01'", "", "B2"},
		{"born BETWEEN '1990-01-01' AND '1990-12-31'", "", "A1"},
		{"born IN ('2001-02-03')", "", "B2"},
		{"seen >= '2026-03-01 00:00:00'", "", "B2"},
		{"seen < '2026-01-02T03:04:06Z'", "", "A1"},
		{"stamp = '2026-01-02T03:04:05Z'", "", "A1"},
	} {
		var ws []string
		if c.where != "" {
			ws = []string{c.where}
		}
		q, err := cliquery.BuildQuery(ws, c.order, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		res, err := a.ExportTableWithQuery(ctx, table, q, "test", "")
		if err != nil {
			t.Errorf("where=%q order=%q: %v", c.where, c.order, err)
			continue
		}
		var codes []string
		for _, r := range res[0].GetRows() {
			codes = append(codes, r[0])
		}
		if got := strings.Join(codes, ","); got != c.want {
			t.Errorf("where=%q order=%q: rows %s, want %s", c.where, c.order, got, c.want)
		}
	}

	back, err := a.ExportTable(ctx, table)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range back[0].GetRows() {
		if r[0] == "A1" && r[2] != "1234.56789012345" {
			t.Errorf("REAL lost precision: 1234.56789012345 came back %q", r[2])
		}
	}
}

// replace/ignore without key fields must refuse BEFORE creating the table:
// Oracle commits DDL implicitly, and the refusal used to come from the row
// insert, after CREATE TABLE had already left an empty table behind.
func TestOracleLiveKeylessReplaceCreatesNothing(t *testing.T) {
	for _, version := range []string{"18", "21"} {
		dsn := os.Getenv("TDTP_ORACLE" + version + "_DSN")
		if dsn == "" {
			t.Run(version+"c", func(t *testing.T) { t.Skip("Oracle test DSN is not configured") })
			continue
		}
		t.Run(version+"c", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			g, err := adapters.New(ctx, adapters.Config{Type: AdapterType, DSN: dsn})
			if err != nil {
				t.Fatal(err)
			}
			a := g.(*Adapter)
			t.Cleanup(func() { _ = a.Close(context.Background()) })
			table := fmt.Sprintf("tdtp_nokey_%d", time.Now().UnixNano()%1_000_000_000)
			t.Cleanup(func() { _ = a.DropTable(context.Background(), table) })
			pkts, err := packet.NewGenerator().GenerateReference(table,
				packet.Schema{Fields: []packet.Field{{Name: "v", Type: "TEXT"}}}, [][]string{{"x"}})
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range []adapters.ImportStrategy{adapters.StrategyReplace, adapters.StrategyIgnore} {
				if err := a.ImportPacket(ctx, pkts[0], s); err == nil || !strings.Contains(err.Error(), "requires key fields") {
					t.Errorf("%s without keys: %v, want a key-fields error", s, err)
				}
				if exists, err := a.TableExists(ctx, table); err != nil || exists {
					t.Errorf("%s without keys left a table behind (exists=%v, err=%v)", s, exists, err)
				}
			}
		})
	}
}
