package commands

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// makeINTestPacket writes a v1.0 TDTP packet with INTEGER id + TEXT status
// columns and returns its path. Used to drive the full --to-csv + WHERE IN
// path end-to-end.
func makeINTestPacket(t *testing.T, rows [][]string) string {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "status", Type: "TEXT"},
		},
	}
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("users", schema, rows)
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	xmlData, err := gen.ToXML(pkts[0], true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}
	path := filepath.Join(t.TempDir(), "in.tdtp.xml")
	if err := os.WriteFile(path, xmlData, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// queryWithWhere builds a packet.Query from a TDTQL WHERE clause, exactly like
// the CLI does for --where.
func queryWithWhere(t *testing.T, where string) *packet.Query {
	t.Helper()
	filters, err := tdtql.NewTranslator().TranslateWhere(where)
	if err != nil {
		t.Fatalf("TranslateWhere(%q): %v", where, err)
	}
	q := packet.NewQuery()
	q.Filters = filters
	return q
}

func readCSVRecords(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open csv: %v", err)
	}
	defer func() { _ = f.Close() }()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	return records
}

// TestToCSV_INFilter_EndToEnd drives the full stack: packet → ConvertTDTPToCSV
// → tdtql.Executor → FilterEngine → Comparator.In (the cached implementation).
// It confirms WHERE IN / NOT IN produce the correct filtered rows over a
// dataset large enough to exercise the per-row list-parse caching.
func TestToCSV_INFilter_EndToEnd(t *testing.T) {
	// 1000 rows cycling through 5 statuses → exercises the IN cache heavily.
	statuses := []string{"active", "deleted", "banned", "pending", "archived"}
	var rows [][]string
	for i := 0; i < 1000; i++ {
		rows = append(rows, []string{
			itoa(i),
			statuses[i%len(statuses)],
		})
	}
	in := makeINTestPacket(t, rows)

	t.Run("string IN", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out.csv")
		err := ConvertTDTPToCSV(context.Background(), CSVOptions{
			InputFile:  in,
			OutputFile: out,
			Delimiter:  ',',
			Query:      queryWithWhere(t, "status IN ('active', 'pending')"),
		})
		if err != nil {
			t.Fatalf("ConvertTDTPToCSV: %v", err)
		}
		recs := readCSVRecords(t, out)
		// header + 400 rows (200 active + 200 pending out of 1000)
		if len(recs) != 401 {
			t.Fatalf("expected 401 records (header+400), got %d", len(recs))
		}
		for _, r := range recs[1:] {
			if r[1] != "active" && r[1] != "pending" {
				t.Errorf("unexpected status survived IN filter: %q", r[1])
			}
		}
	})

	t.Run("integer IN", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out.csv")
		err := ConvertTDTPToCSV(context.Background(), CSVOptions{
			InputFile:  in,
			OutputFile: out,
			Delimiter:  ',',
			Query:      queryWithWhere(t, "id IN (0, 1, 2, 500, 999)"),
		})
		if err != nil {
			t.Fatalf("ConvertTDTPToCSV: %v", err)
		}
		recs := readCSVRecords(t, out)
		if len(recs) != 6 { // header + 5
			t.Fatalf("expected 6 records (header+5), got %d", len(recs))
		}
		got := map[string]bool{}
		for _, r := range recs[1:] {
			got[r[0]] = true
		}
		for _, want := range []string{"0", "1", "2", "500", "999"} {
			if !got[want] {
				t.Errorf("expected id %q in result, missing", want)
			}
		}
	})

	t.Run("NOT IN", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out.csv")
		err := ConvertTDTPToCSV(context.Background(), CSVOptions{
			InputFile:  in,
			OutputFile: out,
			Delimiter:  ',',
			Query:      queryWithWhere(t, "status NOT IN ('deleted', 'banned', 'archived')"),
		})
		if err != nil {
			t.Fatalf("ConvertTDTPToCSV: %v", err)
		}
		recs := readCSVRecords(t, out)
		// 1000 - 600 (deleted+banned+archived) = 400 (active+pending)
		if len(recs) != 401 {
			t.Fatalf("expected 401 records (header+400), got %d", len(recs))
		}
		for _, r := range recs[1:] {
			if r[1] == "deleted" || r[1] == "banned" || r[1] == "archived" {
				t.Errorf("excluded status leaked through NOT IN: %q", r[1])
			}
		}
	})
}

// itoa is a tiny strconv.Itoa wrapper kept local to avoid an extra import in
// the table-building loop above.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
