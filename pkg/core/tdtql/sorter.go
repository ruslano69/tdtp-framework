package tdtql

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// Sorter сортирует данные
type Sorter struct{}

// NewSorter создает новый сортировщик
func NewSorter() *Sorter {
	return &Sorter{}
}

// Sort сортирует строки согласно OrderBy
func (s *Sorter) Sort(
	rows [][]string,
	orderBy *packet.OrderBy,
	schemaObj packet.Schema,
	converter *schema.Converter,
) ([][]string, error) {
	return s.sortWindow(rows, orderBy, schemaObj, converter, 0, len(rows))
}

// sortWindow возвращает строки с позиций [from, to) отсортированного списка —
// то же, что Sort(...)[from:to]. Когда окно прижато к началу или концу и вместе
// с отступом меньше половины таблицы, остальные строки не сортируются (topRows).
// Требует 0 <= from <= to <= len(rows).
func (s *Sorter) sortWindow(
	rows [][]string,
	orderBy *packet.OrderBy,
	schemaObj packet.Schema,
	converter *schema.Converter,
	from, to int,
) ([][]string, error) {

	if orderBy == nil {
		return rows[from:to], nil
	}

	sortFields, err := s.resolveSortFields(orderBy, schemaObj)
	if err != nil {
		return nil, err
	}

	if len(sortFields) == 0 {
		// Копируем срез чтобы не отдавать наружу оригинал
		result := make([][]string, to-from)
		copy(result, rows[from:to])
		return result, nil
	}

	// Каждое значение разбирается один раз, а не при каждом сравнении.
	columns := make([]sortColumn, len(sortFields))
	totalOrder := true
	for i, sf := range sortFields {
		columns[i] = buildSortColumn(rows, sf, converter)
		totalOrder = totalOrder && columns[i].totalOrder
	}

	compare := func(a, b int) int {
		for i := range columns {
			col := &columns[i]
			c := col.compare(a, b)
			if c == 0 {
				continue // равны, проверяем следующее поле
			}
			if col.desc {
				return -c
			}
			return c
		}
		return 0
	}

	n := len(rows)

	// Выборка без полной сортировки даёт тот же ответ только когда сравнение
	// задаёт строгий порядок. Иначе результат зависит от алгоритма, и его надо
	// сохранить прежним.
	if totalOrder {
		// Равные строки идут в исходном порядке — как при стабильной сортировке.
		strict := func(a, b int) int {
			if c := compare(a, b); c != 0 {
				return c
			}
			return cmp.Compare(a, b)
		}
		switch {
		case to < n/2: // начало списка: LIMIT, возможно с OFFSET
			return pickRows(rows, topRows(n, to, strict)[from:]), nil
		case n-from < n/2: // конец списка: LIMIT -N
			tail := topRows(n, n-from, func(a, b int) int { return strict(b, a) })
			slices.Reverse(tail)
			return pickRows(rows, tail[:to-from]), nil
		}
	}

	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, compare)

	return pickRows(rows, order[from:to]), nil
}

// sortField описывает поле для сортировки
type sortField struct {
	index     int
	direction string // ASC или DESC
	field     packet.Field
}

// resolveSortFields находит в схеме поля из OrderBy
func (s *Sorter) resolveSortFields(orderBy *packet.OrderBy, schemaObj packet.Schema) ([]sortField, error) {
	requested := orderBy.Fields
	if orderBy.Field != "" {
		// Простая сортировка по одному полю
		requested = []packet.OrderField{{Name: orderBy.Field, Direction: orderBy.Direction}}
	}

	sortFields := make([]sortField, 0, len(requested))
	for _, f := range requested {
		field, index, err := s.getFieldInfo(f.Name, schemaObj)
		if err != nil {
			return nil, err
		}
		sortFields = append(sortFields, sortField{index: index, direction: f.Direction, field: field})
	}

	return sortFields, nil
}

// getFieldInfo находит информацию о поле
func (s *Sorter) getFieldInfo(fieldName string, schemaObj packet.Schema) (packet.Field, int, error) {
	for i, field := range schemaObj.Fields {
		if strings.EqualFold(field.Name, fieldName) {
			return field, i, nil
		}
	}
	return packet.Field{}, -1, fmt.Errorf("field '%s' not found", fieldName)
}

// sortKey — разобранное значение одной ячейки
type sortKey struct {
	raw     string
	integer int64
	real    float64
	moment  time.Time
	parsed  bool // значение разобрано по типу колонки
	missing bool // в строке нет такой колонки
}

// sortColumn — ключи сортировки по одному полю, по одному на строку
type sortColumn struct {
	kind schema.DataType
	desc bool
	keys []sortKey
	// totalOrder: сравнение по этой колонке задаёт строгий порядок. Это не так,
	// если часть пар сравнивается иначе, чем остальные: неразобранное значение
	// (сравнивается как строка), NaN, boolean кроме 0/1, короткая строка.
	totalOrder bool
}

// buildSortColumn разбирает значения колонки
func buildSortColumn(rows [][]string, sf sortField, converter *schema.Converter) sortColumn {
	col := sortColumn{
		kind:       schema.NormalizeType(schema.DataType(sf.field.Type)),
		desc:       sf.direction == "DESC",
		keys:       make([]sortKey, len(rows)),
		totalOrder: true,
	}

	fieldDef := schema.FieldDef{
		Name:     sf.field.Name,
		Type:     schema.DataType(sf.field.Type),
		Timezone: sf.field.Timezone,
		Nullable: true,
	}

	for i, row := range rows {
		key := &col.keys[i]
		if sf.index >= len(row) {
			key.missing = true
			col.totalOrder = false
			continue
		}

		key.raw = row[sf.index]
		if key.raw == "" {
			continue // NULL, разбирать нечего
		}

		switch col.kind {
		case schema.TypeInteger:
			v, err := strconv.ParseInt(key.raw, 10, 64)
			key.integer, key.parsed = v, err == nil

		case schema.TypeReal, schema.TypeDecimal:
			v, err := strconv.ParseFloat(key.raw, 64)
			key.real, key.parsed = v, err == nil
			if math.IsNaN(v) {
				col.totalOrder = false
			}

		case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
			tv, err := converter.ParseValue(key.raw, fieldDef)
			if err == nil && tv.TimeValue != nil {
				key.moment, key.parsed = *tv.TimeValue, true
			}

		case schema.TypeBoolean:
			key.parsed = key.raw == "0" || key.raw == "1"

		default:
			key.parsed = true // текст сравнивается как есть
		}

		if !key.parsed {
			col.totalOrder = false
		}
	}

	return col
}

// compare сравнивает значения строк с номерами a и b.
// Возвращает: -1 если a < b, 0 если равны, 1 если a > b
func (c *sortColumn) compare(a, b int) int {
	x, y := &c.keys[a], &c.keys[b]
	if x.missing || y.missing {
		return 0
	}

	// NULL обработка
	switch {
	case x.raw == "" && y.raw == "":
		return 0
	case x.raw == "":
		return -1 // NULL меньше любого значения
	case y.raw == "":
		return 1
	}

	switch c.kind {
	case schema.TypeInteger:
		if x.parsed && y.parsed {
			return cmp.Compare(x.integer, y.integer)
		}

	case schema.TypeReal, schema.TypeDecimal:
		if x.parsed && y.parsed {
			// Не cmp.Compare: он ставит NaN перед числами, а здесь NaN равен всему.
			switch {
			case x.real < y.real:
				return -1
			case x.real > y.real:
				return 1
			}
			return 0
		}

	case schema.TypeBoolean:
		// false (0) < true (1)
		switch {
		case x.raw == "0" && y.raw == "1":
			return -1
		case x.raw == "1" && y.raw == "0":
			return 1
		}
		return 0

	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		if x.parsed && y.parsed {
			return x.moment.Compare(y.moment)
		}
	}

	// Текст, остальные типы и неразобранные значения
	return strings.Compare(x.raw, y.raw)
}

// topRows возвращает номера k первых из n строк в порядке compare.
// compare должен задавать строгий порядок. Стоимость O(n log k).
func topRows(n, k int, compare func(a, b int) int) []int {
	if k <= 0 {
		return nil
	}

	// Куча из k лучших; в корне худший из них.
	heap := make([]int, k)
	for i := range heap {
		heap[i] = i
	}
	down := func(i int) {
		for {
			worst := i
			for child := 2*i + 1; child <= 2*i+2 && child < k; child++ {
				if compare(heap[child], heap[worst]) > 0 {
					worst = child
				}
			}
			if worst == i {
				return
			}
			heap[i], heap[worst] = heap[worst], heap[i]
			i = worst
		}
	}
	for i := k/2 - 1; i >= 0; i-- {
		down(i)
	}
	for i := k; i < n; i++ {
		if compare(i, heap[0]) < 0 {
			heap[0] = i
			down(0)
		}
	}

	slices.SortFunc(heap, compare)

	return heap
}

// pickRows собирает строки по номерам
func pickRows(rows [][]string, order []int) [][]string {
	result := make([][]string, len(order))
	for i, idx := range order {
		result[i] = rows[idx]
	}
	return result
}
