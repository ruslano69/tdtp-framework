package commands

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Экранированный "|" в предыдущем поле не должен сдвигать индекс tracking-поля.
func TestExtractLastSyncValue_EscapedDelimiterBeforeTrackingField(t *testing.T) {
	pkt := packet.NewDataPacket(packet.TypeReference, "t")
	pkt.Schema = packet.Schema{Fields: []packet.Field{
		{Name: "note", Type: "TEXT"},
		{Name: "updated", Type: "TEXT"},
	}}
	pkt.Data.Rows = []packet.Row{
		{Value: `a\|b|2026-01-02`},
		{Value: `x\|y|2026-03-04`},
	}

	got, err := extractLastSyncValue([]*packet.DataPacket{pkt}, "updated")
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026-03-04" {
		t.Errorf("got %q, want 2026-03-04", got)
	}
}
