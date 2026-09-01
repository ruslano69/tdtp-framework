package commands

import (
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// makeCompressedPacket собирает сжатый пакет с заданным числом строк и
// заявленным в заголовке счётчиком, который можно намеренно разойтись с
// реальностью.
func makeCompressedPacket(t *testing.T, rows int, declared int) *packet.DataPacket {
	t.Helper()

	values := make([][]string, rows)
	for i := range values {
		values[i] = []string{"1", "value"}
	}
	schema := packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
	}}
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("t", schema, values)
	if err != nil {
		t.Fatal(err)
	}
	pkt := pkts[0]
	pkt.MaterializeRows()

	joined := make([]string, len(pkt.Data.Rows))
	for i, r := range pkt.Data.Rows {
		joined[i] = r.Value
	}
	blob, _, err := processors.CompressDataForTdtp(joined, 3)
	if err != nil {
		t.Fatal(err)
	}
	pkt.Data = packet.Data{
		Compression: "zstd",
		Rows:        []packet.Row{{Value: blob}},
	}
	pkt.Header.RecordsInPart = declared
	return pkt
}

// --test обещает в справке "count rows vs header". На сжатом пакете он этого не
// делал: DryDecompress проверял блоб на целость, а счётчиком объявлялся сам
// заголовок — то есть заявленное число не проверялось, а повторялось. Пакет с
// RecordsInPart=999 и пятью строками внутри проходил проверку и отчитывался о
// 999 строках.
func TestValidatePacket_CompressedRowCountIsChecked(t *testing.T) {
	pkt := makeCompressedPacket(t, 5, 999)

	got, err := validatePacket(pkt, "tampered.tdtp")
	if err == nil {
		t.Fatal("a compressed packet whose header overstates the row count must fail")
	}
	if !strings.Contains(err.Error(), "RecordsInPart mismatch") {
		t.Errorf("error does not name the problem: %v", err)
	}
	// Возвращается настоящее число, а не заявленное — иначе итоговая сумма по
	// частям повторила бы ту же ложь.
	if got != 5 {
		t.Errorf("returned row count = %d, want the real 5 rather than the declared 999", got)
	}
}

// Обратная сторона: честный сжатый пакет обязан проходить, и число строк должно
// браться из распакованных данных.
func TestValidatePacket_CompressedHonestPacketPasses(t *testing.T) {
	for _, n := range []int{1, 5, 100} {
		pkt := makeCompressedPacket(t, n, n)
		got, err := validatePacket(pkt, "honest.tdtp")
		if err != nil {
			t.Errorf("%d rows: honest packet rejected: %v", n, err)
		}
		if got != n {
			t.Errorf("%d rows: counted %d", n, got)
		}
	}
}

// RecordsInPart=0 означает "не заявлено" и проверку не включает — так же, как
// на несжатом пути.
func TestValidatePacket_CompressedUnstatedCountIsNotChecked(t *testing.T) {
	pkt := makeCompressedPacket(t, 5, 0)
	got, err := validatePacket(pkt, "unstated.tdtp")
	if err != nil {
		t.Errorf("RecordsInPart=0 must not trigger the check: %v", err)
	}
	if got != 5 {
		t.Errorf("counted %d, want 5", got)
	}
}
