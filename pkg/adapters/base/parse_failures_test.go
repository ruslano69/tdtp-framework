package base

import (
	"strings"
	"sync"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// A value that does not parse as its field's type still goes into the packet
// as it came — that has not changed. What changed is that it is counted, so
// the export can say so instead of one log line per cell.
func TestParseFailures_CountedPerField(t *testing.T) {
	c := NewUniversalTypeConverter()
	amount := packet.Field{Name: "amount", Type: "DECIMAL", Precision: 10, Scale: 2}
	born := packet.Field{Name: "born", Type: "DATE"}

	for _, v := range []string{"12.50", "abc", "1.234", "7"} {
		if got := c.ConvertValueToTDTP(amount, v); v == "abc" && got != "abc" {
			t.Errorf("unparsable value must still pass through unchanged, got %q", got)
		}
	}
	c.ConvertValueToTDTP(born, "not a date")

	got := c.TakeParseFailures()
	if len(got) != 2 {
		t.Fatalf("failures = %+v, want amount and born", got)
	}
	if f := got[0]; f.Field != "amount" || f.Count != 2 || f.Sample != "abc" || f.Replaced {
		t.Errorf("amount = %+v, want 2 failures, first sample abc", f)
	}
	if f := got[1]; f.Field != "born" || f.Count != 1 || f.Sample != "not a date" {
		t.Errorf("born = %+v", f)
	}
	if again := c.TakeParseFailures(); again != nil {
		t.Errorf("TakeParseFailures must clear the tally, got %+v", again)
	}
}

// json.Marshal failing replaced the value with "{}" — a substitution, now
// recorded as one.
func TestParseFailures_JSONReplacementRecorded(t *testing.T) {
	c := NewUniversalTypeConverter()
	f := packet.Field{Name: "doc", Type: "TEXT", Subtype: "jsonb"}
	if got := c.DBValueToString(map[string]any{"ch": make(chan int)}, f, "postgres"); got != "{}" {
		t.Fatalf("got %q", got)
	}
	failures := c.TakeParseFailures()
	if len(failures) != 1 || !failures[0].Replaced || failures[0].Field != "doc" {
		t.Fatalf("failures = %+v, want one replacement on doc", failures)
	}
	if s := failures[0].String(); !strings.Contains(s, "replaced") {
		t.Errorf("warning should say the value was replaced: %s", s)
	}
}

func TestParseFailures_SampleIsCut(t *testing.T) {
	c := NewUniversalTypeConverter()
	long := strings.Repeat("я", 100)
	c.ConvertValueToTDTP(packet.Field{Name: "n", Type: "DECIMAL"}, long)
	f := c.TakeParseFailures()[0]
	if got := []rune(f.Sample); len(got) != sampleLen+1 || got[sampleLen] != '…' {
		t.Errorf("sample %q: want %d runes and an ellipsis", f.Sample, sampleLen)
	}
}

// Readers may convert concurrently; the tally must be safe under -race.
func TestParseFailures_Concurrent(t *testing.T) {
	c := NewUniversalTypeConverter()
	f := packet.Field{Name: "n", Type: "DECIMAL"} // INTEGER/TEXT/BOOLEAN take an identity fast path and are never parsed
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.ConvertValueToTDTP(f, "x")
			}
		}()
	}
	wg.Wait()
	if got := c.TakeParseFailures(); len(got) != 1 || got[0].Count != 800 {
		t.Errorf("got %+v, want 800 failures on n", got)
	}
}
