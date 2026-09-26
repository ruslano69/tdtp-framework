package main

// diffmerge_cmd_test.go — diff/merge through the dispatcher.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
	"github.com/ruslano69/tdtp-framework/pkg/validate"
)

// writeDiffFixture writes a tiny packet file and returns its path.
func writeDiffFixture(t *testing.T, name string, rows [][]string) string {
	t.Helper()
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("t",
		packet.Schema{Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "v", Type: "TEXT"},
		}}, rows)
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	xmlData, err := gen.ToXML(pkts[0], true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, xmlData, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestDiffCmd_Identical(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"1", "x"}})
	b := writeDiffFixture(t, "b.xml", [][]string{{"1", "x"}})
	code, stdout, _ := runApp(t, "diff", a, b)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "identical") {
		t.Errorf("output should say identical, got:\n%s", stdout)
	}
}

func TestDiffCmd_Differ(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"1", "x"}})
	b := writeDiffFixture(t, "b.xml", [][]string{{"1", "y"}, {"2", "z"}})
	code, stdout, _ := runApp(t, "diff", a, b)
	if code != ExitOK {
		t.Fatalf("exit = %d (differences are the answer, not an error)", code)
	}
	if !strings.Contains(stdout, "differ") {
		t.Errorf("output should say differ, got:\n%s", stdout)
	}
}

func TestDiffCmd_JSON(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"1", "x"}})
	b := writeDiffFixture(t, "b.xml", [][]string{{"1", "y"}})
	code, stdout, _ := runApp(t, "--json", "diff", a, b)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, `"modified":1`) {
		t.Errorf("JSON should report 1 modification, got %q", stdout)
	}
}

func TestDiffCmd_Arity(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"1", "x"}})
	code, _, _ := runApp(t, "diff", a)
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestMergeCmd_Union(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"1", "x"}})
	b := writeDiffFixture(t, "b.xml", [][]string{{"2", "y"}})
	out := filepath.Join(t.TempDir(), "m.xml")
	code, _, _ := runApp(t, "merge", a, b, "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	s := string(data)
	if !strings.Contains(s, "1|x") || !strings.Contains(s, "2|y") {
		t.Errorf("merged file should hold both rows:\n%.400s", s)
	}
}

func TestMergeCmd_NeedsOutput(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"1", "x"}})
	b := writeDiffFixture(t, "b.xml", [][]string{{"2", "y"}})
	code, _, _ := runApp(t, "merge", a, b)
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (output required)", code, ExitUsage)
	}
}

func TestMergeCmd_NeedsTwo(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"1", "x"}})
	code, _, _ := runApp(t, "merge", a, "--output", filepath.Join(t.TempDir(), "m.xml"))
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestCompat_MergeSplitsComma(t *testing.T) {
	got, _, ok := compatResolve([]string{"--merge", "a.xml,b.xml"})
	if !ok || len(got) != 3 || got[0] != "merge" || got[1] != "a.xml" || got[2] != "b.xml" {
		t.Errorf("--merge a,b rewrote to %v", got)
	}
}

func TestMergeCmd_SortDeterministic(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"10", "x"}, {"9", "y"}})
	b := writeDiffFixture(t, "b.xml", [][]string{{"2", "z"}})
	out1 := filepath.Join(t.TempDir(), "s1.xml")
	out2 := filepath.Join(t.TempDir(), "s2.xml")
	for _, out := range []string{out1, out2} {
		code, _, _ := runApp(t, "merge", a, b, "--output", out, "--sort", "id")
		if code != ExitOK {
			t.Fatalf("exit = %d", code)
		}
	}
	norm := func(p string) string {
		data, _ := os.ReadFile(p)
		s := string(data)
		start := 0
		for {
			i := strings.Index(s[start:], "<MessageID>")
			j := strings.Index(s[start:], "</MessageID>")
			if i < 0 || j < 0 {
				break
			}
			s = s[:start+i] + "<MessageID>ID" + s[start+j:]
			start += i + len("<MessageID>ID")
		}
		if i := strings.Index(s, "<Timestamp>"); i >= 0 {
			if j := strings.Index(s, "</Timestamp>"); j >= 0 {
				s = s[:i] + "<Timestamp>TS" + s[j:]
			}
		}
		return s
	}
	if norm(out1) != norm(out2) {
		t.Error("sorted merge must be byte-identical across runs")
	}
	data, _ := os.ReadFile(out1)
	s := string(data)
	i2 := strings.Index(s, "<R>2|")
	i9 := strings.Index(s, "<R>9|")
	i10 := strings.Index(s, "<R>10|")
	if !(i2 >= 0 && i9 > i2 && i10 > i9) {
		t.Errorf("numeric sort by id should order 2,9,10, got:\n%.400s", s)
	}
}

func TestMergeCmd_SortDesc(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"1", "x"}})
	b := writeDiffFixture(t, "b.xml", [][]string{{"2", "y"}})
	out := filepath.Join(t.TempDir(), "d.xml")
	code, _, _ := runApp(t, "merge", a, b, "--output", out, "--sort", "id", "--order", "desc")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	s := string(data)
	if strings.Index(s, "<R>2|") > strings.Index(s, "<R>1|") || !strings.Contains(s, "<R>2|") {
		t.Errorf("desc sort should lead with id=2, got:\n%.400s", s)
	}
}

// TestMergeCmd_SortDatesChronological pins type-awareness for DATE and
// DATETIME columns: chronological order, not lexicographic.
func TestMergeCmd_SortDatesChronological(t *testing.T) {
	mk := func(t *testing.T, name string, rows [][]string) string {
		t.Helper()
		gen := packet.NewGenerator()
		pkts, err := gen.GenerateReference("ev",
			packet.Schema{Fields: []packet.Field{
				{Name: "id", Type: "INTEGER", Key: true},
				{Name: "day", Type: "DATE"},
				{Name: "ts", Type: "DATETIME"},
			}}, rows)
		if err != nil {
			t.Fatal(err)
		}
		xmlData, err := gen.ToXML(pkts[0], true)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, xmlData, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	a := mk(t, "a.xml", [][]string{
		{"2", "2025-12-01", "2025-11-10T15:30:00Z"},
		{"1", "2025-03-01", "2025-11-10T08:00:00Z"},
	})
	b := mk(t, "b.xml", [][]string{{"3", "2025-06-15", "2025-11-10T12:00:00Z"}})
	for _, tc := range []struct{ col, first, last string }{
		{"day", "2025-03-01", "2025-12-01"},
		{"ts", "2025-11-10T08:00:00Z", "2025-11-10T15:30:00Z"},
	} {
		out := filepath.Join(t.TempDir(), tc.col+".xml")
		code, _, _ := runApp(t, "merge", a, b, "--output", out, "--sort", tc.col)
		if code != ExitOK {
			t.Fatalf("sort %s: exit = %d", tc.col, code)
		}
		data, _ := os.ReadFile(out)
		s := string(data)
		if strings.Index(s, tc.first) > strings.Index(s, tc.last) || !strings.Contains(s, tc.first) {
			t.Errorf("sort %s should order %s before %s:\n%.400s", tc.col, tc.first, tc.last, s)
		}
	}
}

func TestMergeCmd_BadSortColumn(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"1", "x"}})
	b := writeDiffFixture(t, "b.xml", [][]string{{"2", "y"}})
	out := filepath.Join(t.TempDir(), "x.xml")
	code, _, _ := runApp(t, "merge", a, b, "--output", out, "--sort", "ghost")
	if code != ExitInvalid {
		t.Errorf("exit = %d, want %d (engine-detected input problem)", code, ExitInvalid)
	}
}

func TestMergeCmd_BadOrder(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"1", "x"}})
	b := writeDiffFixture(t, "b.xml", [][]string{{"2", "y"}})
	out := filepath.Join(t.TempDir(), "x.xml")
	code, _, _ := runApp(t, "merge", a, b, "--output", out, "--sort", "id", "--order", "sideways")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

// TestMergeCmd_CompressedInput pins the blob-merge trap: merging compressed
// files must merge logical rows, not the single opaque blobs (found live:
// two identical 25,908-row kanzi+columnar files merged into 1 row).
func TestMergeCmd_CompressedInput(t *testing.T) {
	mkib := func(t *testing.T, name string, rows [][]string) string {
		t.Helper()
		gen := packet.NewGenerator()
		pkts, err := gen.GenerateReference("t",
			packet.Schema{Fields: []packet.Field{
				{Name: "id", Type: "INTEGER", Key: true},
				{Name: "v", Type: "TEXT"},
			}}, rows)
		if err != nil {
			t.Fatal(err)
		}
		pkt := pkts[0]
		pkt.MaterializeRows()
		plain := make([]string, len(pkt.Data.Rows))
		for i, r := range pkt.Data.Rows {
			plain[i] = r.Value
		}
		blob, _, err := processors.CompressDataForTdtpAlgo(plain, "zstd", 3)
		if err != nil {
			t.Fatal(err)
		}
		pkt.Data.Compression = "zstd"
		pkt.Data.Checksum = processors.ComputeChecksum([]byte(blob))
		pkt.Data.Rows = []packet.Row{{Value: blob}}
		xmlData, err := gen.ToXML(pkt, true)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, xmlData, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	a := mkib(t, "a.xml", [][]string{{"1", "x"}, {"2", "y"}})
	b := mkib(t, "b.xml", [][]string{{"2", "y"}, {"3", "z"}})
	out := filepath.Join(t.TempDir(), "m.xml")
	code, _, _ := runApp(t, "merge", a, b, "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	s := string(data)
	// Output format follows the first file: zstd in, zstd out.
	if !strings.Contains(s, `compression="zstd"`) {
		t.Fatal("merging compressed files should stay compressed (first file wins)")
	}
	pkt, err := packet.NewParser().ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := processors.DecompressPacket(context.Background(), pkt); err != nil {
		t.Fatal(err)
	}
	rows := pkt.GetRows()
	if len(rows) != 3 {
		t.Errorf("merged rows = %d, want 3 logical rows (not 2 blobs)", len(rows))
	}
	joined := make([]string, len(rows))
	for i, r := range rows {
		joined[i] = strings.Join(r, "|")
	}
	for _, want := range []string{"1|x", "2|y", "3|z"} {
		found := false
		for _, j := range joined {
			if j == want {
				found = true
			}
		}
		if !found {
			t.Errorf("merged output should contain %q", want)
		}
	}
}

// TestMergeCmd_StampsFreshIntegrity: merging stamped inputs must not carry
// their (now stale) hashes — the output is recomputed and self-consistent.
func TestMergeCmd_StampsFreshIntegrity(t *testing.T) {
	mk14 := func(t *testing.T, name string, rows [][]string) string {
		t.Helper()
		gen := packet.NewGenerator()
		pkts, err := gen.GenerateReference("t",
			packet.Schema{Fields: []packet.Field{
				{Name: "id", Type: "INTEGER", Key: true},
				{Name: "v", Type: "TEXT"},
			}}, rows)
		if err != nil {
			t.Fatal(err)
		}
		pkt := pkts[0]
		packet.BumpVersion(pkt, "1.4")
		if _, err := packet.ComputeIntegrity(pkt); err != nil {
			t.Fatal(err)
		}
		xmlData, err := gen.ToXML(pkt, true)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, xmlData, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	a := mk14(t, "a.xml", [][]string{{"1", "x"}})
	b := mk14(t, "b.xml", [][]string{{"2", "y"}})
	out := filepath.Join(t.TempDir(), "m.xml")
	code, _, _ := runApp(t, "merge", a, b, "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	rep := validateFileForTest(t, out)
	if !rep.Valid() {
		t.Errorf("merged output should verify cleanly, got: %v", rep.Errors)
	}
	if rep.Version != "1.4" {
		t.Errorf("version = %q, want 1.4 (inputs were stamped)", rep.Version)
	}
}

// validateFileForTest runs the shared validator over a file (same verdict
// the validate command reports).
func validateFileForTest(t *testing.T, path string) validate.Report {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return validate.Validate(data, path)
}

// TestMergeCmd_SortTextIsLexicographic pins type-awareness: a TEXT column
// holding digits sorts as strings ("010" < "10" < "9"), while the INTEGER
// id column in TestMergeCmd_SortDeterministic sorts numerically. A naive
// try-ParseFloat heuristic would order both numerically.
func TestMergeCmd_SortTextIsLexicographic(t *testing.T) {
	mk := func(t *testing.T, name string, rows [][]string) string {
		t.Helper()
		gen := packet.NewGenerator()
		pkts, err := gen.GenerateReference("codes",
			packet.Schema{Fields: []packet.Field{
				{Name: "code", Type: "TEXT", Key: true},
				{Name: "v", Type: "TEXT"},
			}}, rows)
		if err != nil {
			t.Fatal(err)
		}
		xmlData, err := gen.ToXML(pkts[0], true)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, xmlData, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	a := mk(t, "a.xml", [][]string{{"9", "a"}, {"10", "b"}})
	b := mk(t, "b.xml", [][]string{{"010", "c"}})
	out := filepath.Join(t.TempDir(), "t.xml")
	code, _, _ := runApp(t, "merge", a, b, "--output", out, "--sort", "code")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	s := string(data)
	i010 := strings.Index(s, "<R>010|")
	i10 := strings.Index(s, "<R>10|")
	i9 := strings.Index(s, "<R>9|")
	if !(i010 >= 0 && i10 > i010 && i9 > i10) {
		t.Errorf("TEXT sort should order 010,10,9 — got:\n%.400s", s)
	}
}

// A declared NULL marker is NULL, not text: "[NULL]" sorted after "9"
// lexicographically, putting NULLs last while the help promised first.
func TestMergeCmd_SortNullMarkerFirst(t *testing.T) {
	const pkt = `<DataPacket protocol="TDTP" version="1.0"><Header><Type>reference</Type>` +
		`<TableName>t</TableName><MessageID>M</MessageID><PartNumber>1</PartNumber>` +
		`<TotalParts>1</TotalParts><RecordsInPart>3</RecordsInPart><Timestamp>2026-01-01T00:00:00Z</Timestamp></Header>` +
		`<Schema><Field name="id" type="INTEGER" key="true"></Field>` +
		`<Field name="zip" type="TEXT"><SpecialValues><Null marker="[NULL]"></Null></SpecialValues></Field></Schema>` +
		`<Data><R>1|9</R><R>2|[NULL]</R><R>3|010</R></Data></DataPacket>`
	dir := t.TempDir()
	a := filepath.Join(dir, "a.xml")
	b := filepath.Join(dir, "b.xml")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte(pkt), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "m.xml")
	for _, order := range []string{"asc", "desc"} {
		code, _, stderr := runApp(t, "merge", a, b, "--output", out, "--sort", "zip", "--order", order)
		if code != ExitOK {
			t.Fatalf("exit = %d: %s", code, stderr)
		}
		data, _ := os.ReadFile(out)
		s := string(data)
		iNull, i010, i9 := strings.Index(s, "<R>2|"), strings.Index(s, "<R>3|"), strings.Index(s, "<R>1|")
		ok := iNull >= 0 && iNull < i010 && i010 < i9 // asc: NULL, "010", "9"
		if order == "desc" {
			ok = i9 >= 0 && i9 < i010 && i010 < iNull
		}
		if !ok {
			t.Errorf("order %s: NULL placement wrong:\n%.600s", order, s)
		}
	}
}

// A missing input is operational (exit 1), like inspect/test; only a file
// that was read and rejected is a verdict on the data (exit 3).
func TestDiffMerge_ExitCodesForBadInput(t *testing.T) {
	a := writeDiffFixture(t, "a.xml", [][]string{{"1", "x"}})
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.xml")
	garbage := filepath.Join(dir, "garbage.xml")
	if err := os.WriteFile(garbage, []byte("not xml at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "m.xml")
	for _, tc := range []struct {
		argv []string
		want int
	}{
		{[]string{"diff", a, missing}, ExitFail},
		{[]string{"merge", a, missing, "--output", out}, ExitFail},
		{[]string{"diff", a, garbage}, ExitInvalid},
		{[]string{"merge", a, garbage, "--output", out}, ExitInvalid},
	} {
		if code, _, _ := runApp(t, tc.argv...); code != tc.want {
			t.Errorf("%v: exit = %d, want %d", tc.argv[:2], code, tc.want)
		}
	}
}
