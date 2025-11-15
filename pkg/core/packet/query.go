package packet

// Query представляет TDTQL запрос
type Query struct {
	Language string   `xml:"language,attr"`
	Version  string   `xml:"version,attr"`
	Filters  *Filters `xml:"Filters,omitempty"`
	OrderBy  *OrderBy `xml:"OrderBy,omitempty"`
	Limit    int      `xml:"Limit,omitempty"`
	Offset   int      `xml:"Offset,omitempty"`
}

// Filters содержит дерево условий фильтрации
type Filters struct {
	And *LogicalGroup `xml:"And,omitempty"`
	Or  *LogicalGroup `xml:"Or,omitempty"`
}

// LogicalGroup представляет логическую группу условий
type LogicalGroup struct {
	Filters []Filter       `xml:"Filter,omitempty"`
	And     []LogicalGroup `xml:"And,omitempty"`
	Or      []LogicalGroup `xml:"Or,omitempty"`
}

// Filter представляет одно условие фильтрации
type Filter struct {
	Field    string `xml:"field,attr"`
	Operator string `xml:"operator,attr"`
	Value    string `xml:"value,attr"`
	Value2   string `xml:"value2,attr,omitempty"` // для between
}

// OrderBy определяет сортировку
type OrderBy struct {
	Field     string       `xml:"field,attr,omitempty"`
	Direction string       `xml:"direction,attr,omitempty"`
	Fields    []OrderField `xml:"Field,omitempty"` // множественная сортировка
}

// OrderField для множественной сортировки
type OrderField struct {
	Name      string `xml:"name,attr"`
	Direction string `xml:"direction,attr"`
}

// QueryContext содержит контекст выполнения запроса (в response)
type QueryContext struct {
	OriginalQuery     Query             `xml:"OriginalQuery"`
	ExecutionResults  ExecutionResults  `xml:"ExecutionResults"`
	FilterStatistics  *FilterStatistics `xml:"FilterStatistics,omitempty"`
}

// ExecutionResults содержит результаты выполнения
type ExecutionResults struct {
	TotalRecordsInTable  int  `xml:"TotalRecordsInTable"`
	RecordsAfterFilters  int  `xml:"RecordsAfterFilters"`
	RecordsReturned      int  `xml:"RecordsReturned"`
	MoreDataAvailable    bool `xml:"MoreDataAvailable"`
	NextOffset           int  `xml:"NextOffset,omitempty"`
}

// FilterStatistics содержит статистику по фильтрам
type FilterStatistics struct {
	Filters []FilterStat `xml:"Filter,omitempty"`
	Or      []OrStat     `xml:"Or,omitempty"`
}

// FilterStat статистика одного фильтра
type FilterStat struct {
	Field          string `xml:"field,attr"`
	Operator       string `xml:"operator,attr"`
	Value          string `xml:"value,attr"`
	RecordsMatched int    `xml:"recordsMatched,attr"`
}

// OrStat статистика OR группы
type OrStat struct {
	RecordsMatched int          `xml:"recordsMatched,attr"`
	Filters        []FilterStat `xml:"Filter,omitempty"`
}

// NewQuery создает новый TDTQL запрос
func NewQuery() *Query {
	return &Query{
		Language: "TDTQL",
		Version:  "1.0",
	}
}
