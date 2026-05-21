package packet

import (
	"bytes"
	"strings"
	"testing"
)

func TestIsDictToken(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"@W3", true},
		{"@xlink", true},
		{"@a", true},
		{"@a_b_2", true},
		{"@1bad", false},   // must start with letter after @
		{"@", false},       // no body
		{"W3", false},      // no @
		{"@W3 ", false},    // trailing space
		{"@W3-x", false},   // dash not allowed
		{"text @W3 mid", false}, // substring rejected
		{"", false},
	}
	for _, c := range cases {
		if got := IsDictToken(c.s); got != c.want {
			t.Errorf("IsDictToken(%q) = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestValidateDictionary_Limits(t *testing.T) {
	// Too many entries.
	tooMany := make([]DictEntry, MaxDictEntries+1)
	for i := range tooMany {
		tooMany[i] = DictEntry{Short: "@x", Full: "v"}
	}
	if err := ValidateDictionary(tooMany); err == nil {
		t.Error("expected error for >MaxDictEntries, got nil")
	}

	// Invalid short token.
	if err := ValidateDictionary([]DictEntry{{Short: "noAt", Full: "x"}}); err == nil {
		t.Error("expected error for missing @ prefix")
	}

	// Duplicate short.
	dup := []DictEntry{{Short: "@a", Full: "v1"}, {Short: "@a", Full: "v2"}}
	if err := ValidateDictionary(dup); err == nil {
		t.Error("expected error for duplicate short")
	}

	// Duplicate full.
	dup = []DictEntry{{Short: "@a", Full: "v"}, {Short: "@b", Full: "v"}}
	if err := ValidateDictionary(dup); err == nil {
		t.Error("expected error for duplicate full")
	}

	// Short too long.
	longShort := "@" + strings.Repeat("a", MaxDictShortLength)
	if err := ValidateDictionary([]DictEntry{{Short: longShort, Full: "v"}}); err == nil {
		t.Error("expected error for short > MaxDictShortLength")
	}

	// Full too long.
	if err := ValidateDictionary([]DictEntry{{Short: "@a", Full: strings.Repeat("x", MaxDictFullLength+1)}}); err == nil {
		t.Error("expected error for full > MaxDictFullLength")
	}

	// Valid dictionary passes.
	good := []DictEntry{
		{Short: "@W3", Full: "http://www.w3.org/2000/svg"},
		{Short: "@inks", Full: "http://www.inkscape.org/namespaces/inkscape"},
	}
	if err := ValidateDictionary(good); err != nil {
		t.Errorf("valid dictionary rejected: %v", err)
	}
}

func TestExpandContract_WholeCell(t *testing.T) {
	d := []DictEntry{{Short: "@W3", Full: "http://www.w3.org/2000/svg"}}

	// Whole-cell match — expand.
	if got := ExpandDictionary(d, "@W3"); got != "http://www.w3.org/2000/svg" {
		t.Errorf("Expand(@W3) = %q", got)
	}
	// Substring — NOT expanded (this is the key safety property).
	if got := ExpandDictionary(d, "see @W3 docs"); got != "see @W3 docs" {
		t.Errorf("Expand kept text alone: %q", got)
	}
	// Unknown token — pass through unchanged.
	if got := ExpandDictionary(d, "@unknown"); got != "@unknown" {
		t.Errorf("Expand of unknown token mutated value: %q", got)
	}
	// Reverse: contract the URI to its token.
	if got := ContractDictionary(d, "http://www.w3.org/2000/svg"); got != "@W3" {
		t.Errorf("Contract(URI) = %q", got)
	}
	if got := ContractDictionary(d, "http://example.com"); got != "http://example.com" {
		t.Errorf("Contract of unknown URI mutated value: %q", got)
	}
}

// TestRoundTrip_PacketWithDictionary verifies that a v1.4 packet with a
// Dictionary survives XML marshal → parse → re-marshal: entries are
// preserved, version bumps to 1.4, and no other field is corrupted.
func TestRoundTrip_PacketWithDictionary(t *testing.T) {
	schema := Schema{
		Fields: []Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "ns", Type: "TEXT"},
		},
		Dictionary: &Dictionary{
			Entries: []DictEntry{
				{Short: "@W3", Full: "http://www.w3.org/2000/svg"},
				{Short: "@inks", Full: "http://www.inkscape.org/namespaces/inkscape"},
			},
		},
	}
	rows := [][]string{{"1", "@W3"}, {"2", "@inks"}, {"3", "other"}}

	gen := NewGenerator()
	pkts, err := gen.GenerateReference("test", schema, rows)
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	if len(pkts) != 1 {
		t.Fatalf("got %d packets, want 1", len(pkts))
	}
	if pkts[0].Version != "1.4" {
		t.Errorf("packet version = %q, want 1.4 (auto-bump on Dictionary)", pkts[0].Version)
	}

	xmlBytes, err := gen.ToXML(pkts[0], false)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}
	if !bytes.Contains(xmlBytes, []byte(`<Dictionary>`)) {
		t.Errorf("ToXML output missing <Dictionary> element")
	}
	if !bytes.Contains(xmlBytes, []byte(`short="@W3"`)) {
		t.Errorf("ToXML output missing @W3 entry")
	}

	parser := NewParser()
	parsed, err := parser.Parse(bytes.NewReader(xmlBytes))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Schema.Dictionary == nil || len(parsed.Schema.Dictionary.Entries) != 2 {
		t.Fatalf("parsed dictionary missing entries: %+v", parsed.Schema.Dictionary)
	}
	first := parsed.Schema.Dictionary.Entries[0]
	if first.Short != "@W3" || first.Full != "http://www.w3.org/2000/svg" {
		t.Errorf("parsed dictionary[0] corrupted: %+v", first)
	}
}

// TestDowngrade verifies that a v1.4 packet is correctly converted to v1.3.1:
// all token cells are expanded, Dictionary is removed, version is downgraded.
func TestDowngrade(t *testing.T) {
	entries := []DictEntry{
		{Short: "@W3", Full: "http://www.w3.org/2000/svg"},
		{Short: "@inks", Full: "http://www.inkscape.org/namespaces/inkscape"},
	}
	schema := Schema{
		Fields:     []Field{{Name: "ns", Type: "TEXT"}},
		Dictionary: &Dictionary{Entries: entries},
	}
	gen := NewGenerator()
	pkts, err := gen.GenerateReference("test", schema, [][]string{{"@W3"}, {"@inks"}, {"plain"}})
	if err != nil {
		t.Fatal(err)
	}
	pkt := pkts[0]
	if pkt.Version != "1.4" {
		t.Fatalf("expected v1.4 before downgrade, got %q", pkt.Version)
	}

	Downgrade(pkt)

	if pkt.Version != "1.3.1" {
		t.Errorf("expected v1.3.1 after downgrade, got %q", pkt.Version)
	}
	if pkt.Schema.Dictionary != nil {
		t.Errorf("Dictionary should be nil after downgrade")
	}
	// Verify token expansion in rows
	want := []string{"http://www.w3.org/2000/svg", "http://www.inkscape.org/namespaces/inkscape", "plain"}
	parser := NewParser()
	for i, row := range pkt.Data.Rows {
		vals := parser.GetRowValues(row)
		if len(vals) == 0 || vals[0] != want[i] {
			t.Errorf("row[%d]: got %q, want %q", i, vals[0], want[i])
		}
	}
	// Verify XML output has no Dictionary and correct version
	xmlBytes, _ := gen.ToXML(pkt, false)
	if bytes.Contains(xmlBytes, []byte("<Dictionary>")) {
		t.Errorf("downgraded XML must not contain <Dictionary>")
	}
	if !bytes.Contains(xmlBytes, []byte(`version="1.3.1"`)) {
		t.Errorf("downgraded XML must contain version=1.3.1")
	}
}

// TestDowngrade_Noop verifies that Downgrade is a no-op on packets
// that have no Dictionary (already v1.3.1 or v1.0).
func TestDowngrade_Noop(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "id", Type: "INTEGER"}}}
	gen := NewGenerator()
	pkts, _ := gen.GenerateReference("test", schema, [][]string{{"1"}})
	pkt := pkts[0]
	versionBefore := pkt.Version
	Downgrade(pkt) // must not panic or mutate version
	if pkt.Version != versionBefore {
		t.Errorf("Downgrade mutated version of packet without Dictionary: %q → %q", versionBefore, pkt.Version)
	}
}

// TestForwardCompat_NoDictionary verifies that pre-v1.4 generators
// (no Dictionary) still produce v1.0 packets and no <Dictionary> element.
// This locks in backward compatibility: nothing changes for users who
// don't use the feature.
func TestForwardCompat_NoDictionary(t *testing.T) {
	schema := Schema{
		Fields: []Field{{Name: "id", Type: "INTEGER", Key: true}},
	}
	gen := NewGenerator()
	pkts, err := gen.GenerateReference("test", schema, [][]string{{"1"}})
	if err != nil {
		t.Fatal(err)
	}
	if pkts[0].Version != "1.0" {
		t.Errorf("expected v1.0 without dictionary, got %q", pkts[0].Version)
	}
	xmlBytes, _ := gen.ToXML(pkts[0], false)
	if bytes.Contains(xmlBytes, []byte("<Dictionary>")) {
		t.Errorf("empty dictionary should not emit <Dictionary> element")
	}
}
