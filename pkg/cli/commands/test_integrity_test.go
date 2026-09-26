package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// --test is check-integrity in v2 and prints "Integrity check passed", yet it
// compared only row counts and the compression checksum: xxh3 was never
// checked. These pin that it is now, with the rule import applies.

func integrityPacket(t *testing.T, compact bool) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "city", Type: "TEXT"},
	}}
	pkts, err := packet.NewGenerator().GenerateReference("t", schema,
		[][]string{{"1", "Kyiv"}, {"2", "Kyiv"}, {"3", "Lviv"}})
	if err != nil {
		t.Fatal(err)
	}
	pkt := pkts[0]
	if compact { // export chain order: compact → integrity
		if err := packet.ApplyCompact(pkt, []string{"city"}, false); err != nil {
			t.Fatal(err)
		}
	}
	packet.BumpVersion(pkt, "1.4")
	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		t.Fatal(err)
	}
	return pkt
}

func writeTestPacket(t *testing.T, pkt *packet.DataPacket, mutate func(string) string) string {
	t.Helper()
	data, err := packet.NewGenerator().ToXML(pkt, true)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	if mutate != nil {
		doc = mutate(doc)
	}
	path := filepath.Join(t.TempDir(), "p.xml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runTestFile(t *testing.T, path string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := TestFileTo(&buf, context.Background(), path, nil)
	return buf.String(), err
}

func TestTestFile_VerifiesXXH3(t *testing.T) {
	out, err := runTestFile(t, writeTestPacket(t, integrityPacket(t, false), nil))
	if err != nil {
		t.Fatalf("honest v1.4 packet refused: %v\n%s", err, out)
	}
	if !strings.Contains(out, "xxh3 OK") {
		t.Errorf("report should say xxh3 was checked:\n%s", out)
	}
}

func TestTestFile_TamperedRowRefused(t *testing.T) {
	path := writeTestPacket(t, integrityPacket(t, false), func(s string) string {
		return strings.Replace(s, "|Lviv<", "|Odesa<", 1)
	})
	if out, err := runTestFile(t, path); err == nil {
		t.Fatalf("altered row passed as \"Integrity check passed\":\n%s", out)
	}
}

func TestTestFile_VersionWithoutHashesRefused(t *testing.T) {
	pkts, err := packet.NewGenerator().GenerateReference("t",
		packet.Schema{Fields: []packet.Field{{Name: "id", Type: "INTEGER"}}}, [][]string{{"1"}})
	if err != nil {
		t.Fatal(err)
	}
	pkt := pkts[0]
	pkt.Version = "1.4" // relabelled, never stamped
	out, err := runTestFile(t, writeTestPacket(t, pkt, nil))
	if err == nil || !strings.Contains(out, "carries no xxh3") {
		t.Fatalf("relabelled packet must be refused, err=%v\n%s", err, out)
	}
}

// The hash covers FOLDED compact rows. ParseFile unfolds them on read, so the
// first version of this check refused every uncompressed compact+integrity
// packet; --test now parses like import (ParseBytes, compact left folded).
func TestTestFile_CompactIntegrityUncompressed(t *testing.T) {
	out, err := runTestFile(t, writeTestPacket(t, integrityPacket(t, true), nil))
	if err != nil {
		t.Fatalf("honest compact+integrity packet refused: %v\n%s", err, out)
	}
	if !strings.Contains(out, "xxh3 OK") {
		t.Errorf("report should say xxh3 was checked:\n%s", out)
	}
}
