package tdtql

import (
	"fmt"
	"maps"
	"math/rand"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// Эталон — фильтрация тех же строк по одной через ApplyFilters: порция из одной
// строки всегда ниже порога и идёт в один поток.
func filterRowByRow(t *testing.T, filters *packet.Filters, rows [][]string, s packet.Schema) ([]string, map[string]int, error) {
	t.Helper()
	engine, converter := NewFilterEngine(), schema.NewConverter()
	var ids []string
	stats := map[string]int{}
	for i := range rows {
		matched, rowStats, err := engine.ApplyFilters(filters, rows[i:i+1], s, converter)
		if err != nil {
			return nil, nil, err
		}
		for _, row := range matched {
			ids = append(ids, row[0])
		}
		for field, count := range rowStats {
			stats[field] += count
		}
	}
	return ids, stats, nil
}

func parallelSchema() packet.Schema {
	return packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "n", Type: "INTEGER"},
		{Name: "price", Type: "DECIMAL", Precision: 12, Scale: 2},
		{Name: "born", Type: "DATE"},
		{Name: "city", Type: "TEXT"},
	}}
}

func parallelRows(rng *rand.Rand, n int) [][]string {
	cities := []string{"", "Omsk", "Ufa", "Kazan", "Perm"}
	rows := make([][]string, n)
	for i := range rows {
		rows[i] = []string{
			strconv.Itoa(i),
			strconv.Itoa(rng.Intn(100)),
			fmt.Sprintf("%d.%02d", rng.Intn(1000), rng.Intn(100)),
			fmt.Sprintf("%d-0%d-1%d", 1950+rng.Intn(60), 1+rng.Intn(9), rng.Intn(10)),
			cities[rng.Intn(len(cities))],
		}
	}
	return rows
}

// withProcs гарантирует несколько потоков и на машине с одним ядром.
func withProcs(t *testing.T, n int) {
	t.Helper()
	previous := runtime.GOMAXPROCS(n)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
}

func TestApplyFilters_ParallelMatchesSingleThread(t *testing.T) {
	withProcs(t, 7)
	s := parallelSchema()
	f := func(field, op, value string) packet.Filter {
		return packet.Filter{Field: field, Operator: op, Value: value}
	}
	queries := map[string]*packet.Filters{
		"and":     {And: &packet.LogicalGroup{Filters: []packet.Filter{f("n", "gt", "40"), f("city", "like", "%a%")}}},
		"or":      {Or: &packet.LogicalGroup{Filters: []packet.Filter{f("price", "lt", "100"), f("city", "in", "Omsk,Ufa")}}},
		"dates":   {And: &packet.LogicalGroup{Filters: []packet.Filter{{Field: "born", Operator: "between", Value: "1960-01-01", Value2: "1980-01-01"}}}},
		"nothing": {And: &packet.LogicalGroup{Filters: []packet.Filter{f("n", "gt", "1000")}}},
		"all":     {And: &packet.LogicalGroup{Filters: []packet.Filter{f("n", "gte", "0")}}},
		"nested": {And: &packet.LogicalGroup{
			Filters: []packet.Filter{f("city", "is_not_null", "")},
			Or: []packet.LogicalGroup{{
				Filters: []packet.Filter{f("n", "lt", "10")},
				And:     []packet.LogicalGroup{{Filters: []packet.Filter{f("price", "gt", "500"), f("n", "ne", "55")}}},
			}},
		}},
	}

	rng := rand.New(rand.NewSource(1))
	sizes := []int{0, 1, parallelFilterThreshold - 1, parallelFilterThreshold, parallelFilterThreshold + 1, 5000, 20011}
	for _, size := range sizes {
		rows := parallelRows(rng, size)
		for name, filters := range queries {
			wantIDs, wantStats, err := filterRowByRow(t, filters, rows, s)
			if err != nil {
				t.Fatal(err)
			}
			got, stats, err := NewFilterEngine().ApplyFilters(filters, rows, s, schema.NewConverter())
			if err != nil {
				t.Fatalf("%s/%d: %v", name, size, err)
			}
			if got == nil {
				t.Fatalf("%s/%d: result must be empty, not nil", name, size)
			}
			gotIDs := make([]string, len(got))
			for i, row := range got {
				gotIDs[i] = row[0]
			}
			if !slices.Equal(gotIDs, wantIDs) {
				t.Fatalf("%s/%d: rows differ: got %d, want %d", name, size, len(gotIDs), len(wantIDs))
			}
			if !maps.Equal(stats, wantStats) {
				t.Fatalf("%s/%d: stats %v, want %v", name, size, stats, wantStats)
			}
		}
	}
}

// Две плохие строки в разных порциях: вернуться должна ошибка той, что раньше,
// сколько бы раз ни запускать.
func TestApplyFilters_ParallelReportsFirstBadRow(t *testing.T) {
	withProcs(t, 8)
	rows := parallelRows(rand.New(rand.NewSource(2)), 8000)
	rows[3100][1] = "first-bad"
	rows[7900][1] = "second-bad"
	rows[7000] = rows[7000][:1] // ошибка другого вида, тоже позже первой
	filters := &packet.Filters{And: &packet.LogicalGroup{Filters: []packet.Filter{{Field: "n", Operator: "gt", Value: "1"}}}}

	for range 50 {
		got, stats, err := NewFilterEngine().ApplyFilters(filters, rows, parallelSchema(), schema.NewConverter())
		if err == nil || !strings.Contains(err.Error(), "first-bad") {
			t.Fatalf("want the error of the first bad row, got %v", err)
		}
		if got != nil || stats != nil {
			t.Fatal("no partial result on error")
		}
	}
}

// Один Executor из нескольких горутин: кеши LIKE и IN общие на весь процесс.
// Имеет смысл с -race.
func TestApplyFilters_ParallelSharedExecutor(t *testing.T) {
	withProcs(t, 8)
	rows := parallelRows(rand.New(rand.NewSource(3)), 4000)
	executor := NewExecutor()

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			query := &packet.Query{Filters: &packet.Filters{And: &packet.LogicalGroup{Filters: []packet.Filter{
				{Field: "city", Operator: "like", Value: fmt.Sprintf("%%%c%%", 'a'+i%4)},
				{Field: "n", Operator: "in", Value: fmt.Sprintf("%d,%d,%d", i, i+10, i+20)},
			}}}}
			if _, err := executor.Execute(query, rows, parallelSchema()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
