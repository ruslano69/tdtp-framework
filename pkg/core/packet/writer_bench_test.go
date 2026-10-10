package packet

import (
	"strconv"
	"testing"
)

// BenchmarkWritePacket measures serialising a 10k×10 packet with ToXML —
// the hand-written row path plus the per-value XML text check.
func BenchmarkWritePacket(b *testing.B) {
	fields := make([]Field, 10)
	for i := range fields {
		fields[i] = Field{Name: "col" + strconv.Itoa(i), Type: "TEXT"}
	}
	rows := make([][]string, 10000)
	for r := range rows {
		row := make([]string, 10)
		for c := range row {
			row[c] = "значение_" + strconv.Itoa(r) + "_" + strconv.Itoa(c)
		}
		rows[r] = row
	}
	g := NewGenerator()
	pkts, err := g.GenerateReference("bench", Schema{Fields: fields}, rows)
	if err != nil {
		b.Fatal(err)
	}
	data, _ := g.ToXML(pkts[0], false)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := g.ToXML(pkts[0], false); err != nil {
			b.Fatal(err)
		}
	}
}
