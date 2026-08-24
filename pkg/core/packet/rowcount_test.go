package packet

import (
	"context"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func rowCountPacket(t *testing.T, version string, rows [][]string) (*DataPacket, *Generator) {
	t.Helper()
	g := NewGenerator()
	pkts, err := g.GenerateReference("T", Schema{Fields: []Field{{Name: "ID", Type: "INTEGER"}}}, rows)
	if err != nil {
		t.Fatal(err)
	}
	pkt := pkts[0]
	pkt.Version = version
	return pkt, g
}

// Пакет v1.4, врущий о числе строк, обязан быть отвергнут.
//
// Раньше он проходил: parser.go пропускал сверку начиная с v1.4, а хеши
// целостности заголовок не накрывают — computeHashes хеширует Schema и
// значения строк. Счётчик при этом читают как авторитетный (etl складывает из
// него TotalRowsLoaded, libtdtp отдаёт как число строк).
func TestVerifyRowCount_RejectsLyingHeaderAtV14(t *testing.T) {
	for _, version := range []string{"1.0", "1.3.1", "1.4", "1.5", "2.0"} {
		t.Run(version, func(t *testing.T) {
			pkt, g := rowCountPacket(t, version, [][]string{{"1"}, {"2"}, {"3"}})
			pkt.MaterializeRows()
			pkt.Header.RecordsInPart = 999

			xml, err := g.ToXML(pkt, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewParser().ParseBytes(xml); err == nil {
				t.Errorf("пакет с RecordsInPart=999 при 3 строках принят")
			}
		})
	}
}

// И тот же пакет не должен считаться проверенным по целостности.
func TestVerifyIntegrity_RejectsLyingHeader(t *testing.T) {
	pkt, _ := rowCountPacket(t, "1.4", [][]string{{"1"}, {"2"}, {"3"}})
	pkt.MaterializeRows()
	if _, err := ComputeIntegrity(pkt); err != nil {
		t.Fatal(err)
	}
	pkt.Header.RecordsInPart = 999

	if err := VerifyIntegrity(pkt); err == nil {
		t.Error("VerifyIntegrity подтвердил пакет с неверным счётчиком строк")
	} else if !strings.Contains(err.Error(), "RecordsInPart") {
		t.Errorf("отказ не про счётчик: %v", err)
	}
}

// Честный счётчик проходит на всех версиях — проверка не должна ломать норму.
func TestVerifyRowCount_AcceptsHonestHeader(t *testing.T) {
	for _, version := range []string{"1.0", "1.3.1", "1.4", "1.5"} {
		pkt, g := rowCountPacket(t, version, [][]string{{"1"}, {"2"}})
		pkt.MaterializeRows()
		xml, err := g.ToXML(pkt, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewParser().ParseBytes(xml); err != nil {
			t.Errorf("%s: честный пакет отвергнут: %v", version, err)
		}
	}
}

// Сжатый пакет: до распаковки сверять нечего, после — обязательно.
func TestVerifyRowCount_AfterDecompression(t *testing.T) {
	rows := []string{"1", "2", "3"}
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	blob := enc.EncodeAll([]byte(strings.Join(rows, "\n")), nil)
	_ = enc.Close()

	pkt := NewDataPacket(TypeReference, "T")
	pkt.Version = "1.4"
	pkt.Schema = Schema{Fields: []Field{{Name: "ID", Type: "INTEGER"}}}
	pkt.Header.RecordsInPart = 999 // ложь
	pkt.Data = Data{Compression: "zstd", Rows: []Row{{Value: string(blob)}}}

	// Пока сжато — проверка молчит, строк ещё нет.
	if err := VerifyRowCount(pkt); err != nil {
		t.Fatalf("на сжатом пакете ожидался no-op: %v", err)
	}

	decomp := func(_ context.Context, compressed, _ string) ([]string, error) {
		dec, err := zstd.NewReader(nil)
		if err != nil {
			return nil, err
		}
		defer dec.Close()
		out, err := dec.DecodeAll([]byte(compressed), nil)
		if err != nil {
			return nil, err
		}
		return strings.Split(string(out), "\n"), nil
	}
	if err := NewParser().DecompressData(context.Background(), pkt, decomp); err == nil {
		t.Error("распаковка приняла пакет с RecordsInPart=999 при 3 строках")
	} else if !strings.Contains(err.Error(), "RecordsInPart") {
		t.Errorf("отказ не про счётчик: %v", err)
	}
}
