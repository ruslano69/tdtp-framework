package tdtql

import (
	"fmt"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
)

// ExecutionResult результат выполнения запроса
type ExecutionResult struct {
	FilteredRows    [][]string               // отфильтрованные и отсортированные строки
	TotalRows       int                      // всего строк в исходных данных
	MatchedRows     int                      // строк после фильтрации (до LIMIT)
	ReturnedRows    int                      // строк возвращено (после LIMIT/OFFSET)
	MoreAvailable   bool                     // есть ли еще данные
	NextOffset      int                      // следующий offset для пагинации
	FilterStats     map[string]int           // статистика по фильтрам
	QueryContext    *packet.QueryContext     // контекст для Response
}

// Executor выполняет TDTQL запросы на данных
type Executor struct {
	filter     *FilterEngine
	sorter     *Sorter
	validator  *schema.Validator
	converter  *schema.Converter
}

// NewExecutor создает новый executor
func NewExecutor() *Executor {
	return &Executor{
		filter:    NewFilterEngine(),
		sorter:    NewSorter(),
		validator: schema.NewValidator(),
		converter: schema.NewConverter(),
	}
}

// Execute выполняет Query на данных
func (e *Executor) Execute(query *packet.Query, rows [][]string, schemaObj packet.Schema) (*ExecutionResult, error) {
	result := &ExecutionResult{
		TotalRows:   len(rows),
		FilterStats: make(map[string]int),
	}

	// Валидация схемы
	if err := e.validator.ValidateSchema(schemaObj); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	// 1. Фильтрация
	filteredRows := rows
	if query.Filters != nil {
		var err error
		filteredRows, result.FilterStats, err = e.filter.ApplyFilters(query.Filters, rows, schemaObj, e.converter)
		if err != nil {
			return nil, fmt.Errorf("filter error: %w", err)
		}
	}
	result.MatchedRows = len(filteredRows)

	// 2. Сортировка
	if query.OrderBy != nil {
		var err error
		filteredRows, err = e.sorter.Sort(filteredRows, query.OrderBy, schemaObj, e.converter)
		if err != nil {
			return nil, fmt.Errorf("sort error: %w", err)
		}
	}

	// 3. Пагинация (OFFSET, LIMIT)
	offset := query.Offset
	limit := query.Limit

	if offset < 0 {
		offset = 0
	}

	// Применяем OFFSET
	if offset > 0 {
		if offset >= len(filteredRows) {
			filteredRows = [][]string{}
		} else {
			filteredRows = filteredRows[offset:]
		}
	}

	// Применяем LIMIT
	if limit > 0 && limit < len(filteredRows) {
		filteredRows = filteredRows[:limit]
		result.MoreAvailable = true
		result.NextOffset = offset + limit
	} else {
		result.MoreAvailable = false
	}

	result.FilteredRows = filteredRows
	result.ReturnedRows = len(filteredRows)

	// 4. Создаем QueryContext для stateless
	if query != nil {
		result.QueryContext = e.buildQueryContext(query, result)
	}

	return result, nil
}

// ExecuteWhere выполняет только фильтрацию (без сортировки и пагинации)
func (e *Executor) ExecuteWhere(filters *packet.Filters, rows [][]string, schemaObj packet.Schema) ([][]string, error) {
	if filters == nil {
		return rows, nil
	}

	filteredRows, _, err := e.filter.ApplyFilters(filters, rows, schemaObj, e.converter)
	return filteredRows, err
}

// buildQueryContext создает QueryContext для Response
func (e *Executor) buildQueryContext(query *packet.Query, result *ExecutionResult) *packet.QueryContext {
	return &packet.QueryContext{
		OriginalQuery: *query,
		ExecutionResults: packet.ExecutionResults{
			TotalRecordsInTable: result.TotalRows,
			RecordsAfterFilters: result.MatchedRows,
			RecordsReturned:     result.ReturnedRows,
			MoreDataAvailable:   result.MoreAvailable,
			NextOffset:          result.NextOffset,
		},
		// FilterStatistics можно добавить позже
	}
}

// ValidateQuery проверяет корректность запроса относительно схемы
func (e *Executor) ValidateQuery(query *packet.Query, schemaObj packet.Schema) error {
	// Проверка схемы
	if err := e.validator.ValidateSchema(schemaObj); err != nil {
		return err
	}

	// Проверка полей в фильтрах
	if query.Filters != nil {
		if err := e.validateFiltersFields(query.Filters, schemaObj); err != nil {
			return err
		}
	}

	// Проверка полей в OrderBy
	if query.OrderBy != nil {
		if err := e.validateOrderByFields(query.OrderBy, schemaObj); err != nil {
			return err
		}
	}

	return nil
}

// validateFiltersFields проверяет что все поля из фильтров есть в схеме
func (e *Executor) validateFiltersFields(filters *packet.Filters, schemaObj packet.Schema) error {
	if filters.And != nil {
		if err := e.validateLogicalGroupFields(filters.And, schemaObj); err != nil {
			return err
		}
	}
	if filters.Or != nil {
		if err := e.validateLogicalGroupFields(filters.Or, schemaObj); err != nil {
			return err
		}
	}
	return nil
}

// validateLogicalGroupFields проверяет LogicalGroup
func (e *Executor) validateLogicalGroupFields(group *packet.LogicalGroup, schemaObj packet.Schema) error {
	// Проверка фильтров
	for _, filter := range group.Filters {
		if _, err := e.validator.GetFieldByName(schemaObj, filter.Field); err != nil {
			return fmt.Errorf("field '%s' not found in schema", filter.Field)
		}
	}

	// Рекурсивная проверка вложенных групп
	for _, andGroup := range group.And {
		if err := e.validateLogicalGroupFields(&andGroup, schemaObj); err != nil {
			return err
		}
	}
	for _, orGroup := range group.Or {
		if err := e.validateLogicalGroupFields(&orGroup, schemaObj); err != nil {
			return err
		}
	}

	return nil
}

// validateOrderByFields проверяет поля в OrderBy
func (e *Executor) validateOrderByFields(orderBy *packet.OrderBy, schemaObj packet.Schema) error {
	if orderBy.Field != "" {
		if _, err := e.validator.GetFieldByName(schemaObj, orderBy.Field); err != nil {
			return fmt.Errorf("order by field '%s' not found in schema", orderBy.Field)
		}
	}

	for _, field := range orderBy.Fields {
		if _, err := e.validator.GetFieldByName(schemaObj, field.Name); err != nil {
			return fmt.Errorf("order by field '%s' not found in schema", field.Name)
		}
	}

	return nil
}
