package commands

import (
	"context"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Замаскированные колонки перетипизируются в TEXT (длина/точность сброшены),
// остальные колонки схемы не затрагиваются, исходный срез Fields не мутируется.
func TestRowProcessors_MaskedColumnsBecomeText(t *testing.T) {
	pkt := packet.NewDataPacket(packet.TypeReference, "t")
	orig := []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "ssn", Type: "INTEGER"},
		{Name: "email", Type: "VARCHAR", Length: 6},
		{Name: "born", Type: "DATE"},
	}
	pkt.Schema = packet.Schema{Fields: append([]packet.Field(nil), orig...)}
	pkt.Data.Rows = []packet.Row{{Value: `1|3012345678|a@b.ua|2000-01-02`}}

	pm := NewRowProcessors()
	if err := pm.AddMaskProcessor("ssn,email"); err != nil {
		t.Fatal(err)
	}
	if err := pm.ProcessPacket(context.Background(), pkt); err != nil {
		t.Fatal(err)
	}

	f := pkt.Schema.Fields
	if f[0].Type != "INTEGER" || !f[0].Key || f[3].Type != "DATE" {
		t.Errorf("unmasked columns changed: %+v", f)
	}
	for _, i := range []int{1, 2} {
		if f[i].Type != "TEXT" || f[i].Length != 0 {
			t.Errorf("masked column %q: want TEXT/len 0, got %s/%d", f[i].Name, f[i].Type, f[i].Length)
		}
	}
	if orig[1].Type != "INTEGER" {
		t.Errorf("original field slice was mutated: %+v", orig[1])
	}
}
