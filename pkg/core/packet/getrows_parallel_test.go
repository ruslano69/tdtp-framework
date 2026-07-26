package packet

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// serialGetRows — эталон: последовательный разбор, как было до параллельного.
func serialGetRows(pkt *DataPacket) [][]string {
	parser := NewParser()
	rows := make([][]string, len(pkt.Data.Rows))
	for i := range pkt.Data.Rows {
		rows[i] = parser.GetRowValues(pkt.Data.Rows[i])
	}
	return rows
}

// packetWithJoinedRows собирает пакет из готовых pipe-joined строк — ровно та
// форма, в которой Data оказывается и после обычного разбора, и после распаковки.
func packetWithJoinedRows(joined []string) *DataPacket {
	pkt := NewDataPacket(TypeReference, "t")
	pkt.Header.PartNumber, pkt.Header.TotalParts = 1, 1
	pkt.Header.RecordsInPart = len(joined)
	pkt.Data.Rows = make([]Row, len(joined))
	for i, s := range joined {
		pkt.Data.Rows[i] = Row{Value: s}
	}
	return pkt
}

// TestGetRows_ParallelMatchesSerial — параллельный разбор обязан совпадать с
// последовательным на размерах по обе стороны порога.
func TestGetRows_ParallelMatchesSerial(t *testing.T) {
	sizes := []int{
		0,
		1,
		parallelGetRowsThreshold - 1, // последовательная ветка
		parallelGetRowsThreshold,     // параллельная ветка
		parallelGetRowsThreshold + 1,
		5000,
	}

	for _, n := range sizes {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			joined := make([]string, n)
			for i := range joined {
				joined[i] = "a" + strconv.Itoa(i) + "|b" + strconv.Itoa(i) + "|c"
			}
			pkt := packetWithJoinedRows(joined)

			want := serialGetRows(pkt)
			got := pkt.GetRows()

			if !reflect.DeepEqual(got, want) {
				t.Errorf("parallel result differs from serial for %d rows", n)
			}
		})
	}
}

// TestGetRows_OrderIsDeterministic — результат пишется по индексу, поэтому
// порядок не должен зависеть от планировщика. Прогоняем многократно.
func TestGetRows_OrderIsDeterministic(t *testing.T) {
	const n = 4000
	joined := make([]string, n)
	for i := range joined {
		joined[i] = "row" + strconv.Itoa(i) + "|second|third"
	}
	pkt := packetWithJoinedRows(joined)

	first := pkt.GetRows()
	for attempt := 0; attempt < 20; attempt++ {
		if !reflect.DeepEqual(pkt.GetRows(), first) {
			t.Fatalf("GetRows returned a different order on attempt %d", attempt)
		}
	}

	// И порядок обязан соответствовать исходному.
	for i := 0; i < n; i++ {
		if first[i][0] != "row"+strconv.Itoa(i) {
			t.Fatalf("row %d out of place: %q", i, first[i][0])
		}
	}
}

// TestGetRows_ParallelHandlesEscaping — медленный путь GetRowValues (со
// escape-последовательностями) должен работать в горутинах так же.
func TestGetRows_ParallelHandlesEscaping(t *testing.T) {
	const n = 2000
	joined := make([]string, n)
	for i := range joined {
		// Чередуем простые и экранированные строки, чтобы задеть обе ветки.
		if i%2 == 0 {
			joined[i] = `a\|b|c\\d|plain`
		} else {
			joined[i] = "x" + strconv.Itoa(i) + "|y|z"
		}
	}
	pkt := packetWithJoinedRows(joined)

	want := serialGetRows(pkt)
	got := pkt.GetRows()

	if !reflect.DeepEqual(got, want) {
		t.Fatal("escaped values differ between parallel and serial")
	}
	if got[0][0] != "a|b" || got[0][1] != `c\d` {
		t.Errorf("escaping broken: %q", got[0])
	}
}

// TestGetRows_RawRowsShortCircuit — fast-path с rawRows не должен затрагиваться.
func TestGetRows_RawRowsShortCircuit(t *testing.T) {
	rows := make([][]string, 1000)
	for i := range rows {
		rows[i] = []string{"a" + strconv.Itoa(i), "b", "c"}
	}

	fields := []Field{{Name: "c0", Type: "TEXT"}, {Name: "c1", Type: "TEXT"}, {Name: "c2", Type: "TEXT"}}
	pkts, err := NewGenerator().GenerateReference("t", Schema{Fields: fields}, rows)
	if err != nil {
		t.Fatal(err)
	}

	got := pkts[0].GetRows()
	if !reflect.DeepEqual(got, rows) {
		t.Error("rawRows fast path must return the original rows unchanged")
	}
}

// TestGetRows_WideRows — много полей в строке, чтобы работа на горутину была
// неравномерной по длине.
func TestGetRows_WideRows(t *testing.T) {
	const n = 1500
	joined := make([]string, n)
	for i := range joined {
		width := 3
		if i%7 == 0 {
			width = 60 // часть строк заметно длиннее
		}
		parts := make([]string, width)
		for j := range parts {
			parts[j] = "v" + strconv.Itoa(i) + "_" + strconv.Itoa(j)
		}
		joined[i] = strings.Join(parts, "|")
	}
	pkt := packetWithJoinedRows(joined)

	if !reflect.DeepEqual(pkt.GetRows(), serialGetRows(pkt)) {
		t.Error("uneven row widths broke the parallel split")
	}
}
