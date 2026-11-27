package tdtql

import (
	"fmt"
	"math/rand"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// Эталон — прежняя реализация: sort.SliceStable с разбором значений в каждом
// сравнении, а пагинация — после полной сортировки. Сортировка по заранее
// разобранным ключам и выборка начала или конца списка обязаны давать те же
// строки в том же порядке.

func referenceCompare(a, b string, field packet.Field, conv *schema.Converter) int {
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return -1
	case b == "":
		return 1
	}
	switch kind := schema.NormalizeType(schema.DataType(field.Type)); kind {
	case schema.TypeInteger:
		x, err1 := strconv.ParseInt(a, 10, 64)
		y, err2 := strconv.ParseInt(b, 10, 64)
		if err1 == nil && err2 == nil {
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		}
	case schema.TypeReal, schema.TypeDecimal:
		x, err1 := strconv.ParseFloat(a, 64)
		y, err2 := strconv.ParseFloat(b, 64)
		if err1 == nil && err2 == nil {
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		}
	case schema.TypeBoolean:
		switch {
		case a == "0" && b == "1":
			return -1
		case a == "1" && b == "0":
			return 1
		}
		return 0
	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		def := schema.FieldDef{Name: field.Name, Type: schema.DataType(field.Type), Nullable: true}
		x, err1 := conv.ParseValue(a, def)
		y, err2 := conv.ParseValue(b, def)
		if err1 == nil && err2 == nil && x.TimeValue != nil && y.TimeValue != nil {
			return x.TimeValue.Compare(*y.TimeValue)
		}
	}
	return strings.Compare(a, b)
}

func referenceSort(rows [][]string, order []packet.OrderField, s packet.Schema) [][]string {
	conv := schema.NewConverter()
	out := slices.Clone(rows)
	sort.SliceStable(out, func(i, j int) bool {
		for _, o := range order {
			idx := slices.IndexFunc(s.Fields, func(f packet.Field) bool { return f.Name == o.Name })
			if idx >= len(out[i]) || idx >= len(out[j]) {
				continue
			}
			c := referenceCompare(out[i][idx], out[j][idx], s.Fields[idx], conv)
			if c == 0 {
				continue
			}
			if o.Direction == "DESC" {
				return c > 0
			}
			return c < 0
		}
		return false
	})
	return out
}

func referencePage(rows [][]string, offset, limit int) (page [][]string, more bool, next int) {
	if offset > 0 {
		rows = rows[min(offset, len(rows)):]
	}
	switch {
	case limit > 0 && limit < len(rows):
		return rows[:limit], true, max(offset, 0) + limit
	case limit < 0 && -limit < len(rows):
		return rows[len(rows)+limit:], false, 0
	}
	return rows, false, 0
}

func rowIDs(rows [][]string) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r[0])
		b.WriteByte(',')
	}
	return b.String()
}

func TestSortWindow_MatchesFullStableSort(t *testing.T) {
	s := packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "i", Type: "INTEGER"},
		{Name: "r", Type: "REAL"},
		{Name: "d", Type: "DATE"},
		{Name: "b", Type: "BOOLEAN"},
		{Name: "t", Type: "TEXT"},
	}}
	// Мало разных значений — много равных ключей, где виден порядок равных.
	clean := [][]string{
		{"", "-5", "0", "7", "10", "100"},
		{"", "-1.5", "0", "0.0", "2.25", "100"},
		{"", "2024-01-01", "2024-02-29", "2025-12-31"},
		{"", "0", "1"},
		{"", "a", "B", "я", "10", "9"},
	}
	// Значения, ломающие строгий порядок: выборка должна отступить.
	dirty := [][]string{
		{"[NULL]", "x"},
		{"NaN", "[NULL]"},
		{"[NULL]", "not a date"},
		{"x", "true"},
		{"[NULL]"},
	}

	rng := rand.New(rand.NewSource(1))
	exec := NewExecutor()

	for round := range 3000 {
		withDirty := round%3 == 0
		rows := make([][]string, rng.Intn(120))
		for i := range rows {
			row := []string{strconv.Itoa(i)}
			for c, values := range clean {
				v := values[rng.Intn(len(values))]
				if withDirty && rng.Intn(15) == 0 {
					v = dirty[c][rng.Intn(len(dirty[c]))]
				}
				row = append(row, v)
			}
			rows[i] = row
		}

		order := make([]packet.OrderField, 1+rng.Intn(3))
		for i := range order {
			order[i] = packet.OrderField{
				Name:      s.Fields[1+rng.Intn(len(s.Fields)-1)].Name,
				Direction: []string{"ASC", "DESC"}[rng.Intn(2)],
			}
		}

		query := &packet.Query{OrderBy: &packet.OrderBy{Fields: order}}
		if rng.Intn(4) > 0 {
			query.Limit = rng.Intn(161) - 80
		}
		if rng.Intn(2) == 0 {
			query.Offset = rng.Intn(140)
		}

		input := slices.Clone(rows)
		got, err := exec.Execute(query, rows, s)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		want, more, next := referencePage(referenceSort(rows, order, s), query.Offset, query.Limit)

		desc := fmt.Sprintf("round %d: rows=%d order=%v limit=%d offset=%d dirty=%v",
			round, len(rows), order, query.Limit, query.Offset, withDirty)
		if g, w := rowIDs(got.FilteredRows), rowIDs(want); g != w {
			t.Fatalf("%s\n got %s\nwant %s", desc, g, w)
		}
		if got.MoreAvailable != more || got.NextOffset != next || got.ReturnedRows != len(want) {
			t.Fatalf("%s: more=%v next=%d returned=%d, want %v %d %d", desc,
				got.MoreAvailable, got.NextOffset, got.ReturnedRows, more, next, len(want))
		}
		if rowIDs(rows) != rowIDs(input) {
			t.Fatalf("%s: input rows were reordered", desc)
		}
	}
}
