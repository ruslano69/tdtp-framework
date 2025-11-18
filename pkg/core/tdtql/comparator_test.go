package tdtql

import (
	"testing"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
)

func TestComparator_Equals(t *testing.T) {
	comp := NewComparator()
	converter := schema.NewConverter()

	tests := []struct {
		name        string
		field       schema.FieldDef
		rowValue    string
		filterValue string
		expected    bool
	}{
		{
			name:        "Integer equals",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "123",
			filterValue: "123",
			expected:    true,
		},
		{
			name:        "Integer not equals",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "123",
			filterValue: "456",
			expected:    false,
		},
		{
			name:        "Real equals",
			field:       schema.FieldDef{Name: "balance", Type: "REAL"},
			rowValue:    "123.45",
			filterValue: "123.45",
			expected:    true,
		},
		{
			name:        "Text equals",
			field:       schema.FieldDef{Name: "name", Type: "TEXT"},
			rowValue:    "John",
			filterValue: "John",
			expected:    true,
		},
		{
			name:        "Text not equals",
			field:       schema.FieldDef{Name: "name", Type: "TEXT"},
			rowValue:    "John",
			filterValue: "Jane",
			expected:    false,
		},
		{
			name:        "Boolean equals true",
			field:       schema.FieldDef{Name: "is_active", Type: "BOOLEAN"},
			rowValue:    "1",
			filterValue: "1",
			expected:    true,
		},
		{
			name:        "Boolean equals false",
			field:       schema.FieldDef{Name: "is_active", Type: "BOOLEAN"},
			rowValue:    "0",
			filterValue: "0",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := comp.Equals(tt.rowValue, tt.filterValue, tt.field, converter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestComparator_GreaterThan(t *testing.T) {
	comp := NewComparator()
	converter := schema.NewConverter()

	tests := []struct {
		name        string
		field       schema.FieldDef
		rowValue    string
		filterValue string
		expected    bool
	}{
		{
			name:        "Integer greater",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "200",
			filterValue: "100",
			expected:    true,
		},
		{
			name:        "Integer not greater",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "100",
			filterValue: "200",
			expected:    false,
		},
		{
			name:        "Real greater",
			field:       schema.FieldDef{Name: "balance", Type: "REAL"},
			rowValue:    "123.50",
			filterValue: "123.45",
			expected:    true,
		},
		{
			name:        "Negative numbers",
			field:       schema.FieldDef{Name: "balance", Type: "REAL"},
			rowValue:    "-10",
			filterValue: "-20",
			expected:    true,
		},
		{
			name:        "Text greater (lexicographic)",
			field:       schema.FieldDef{Name: "name", Type: "TEXT"},
			rowValue:    "Bob",
			filterValue: "Alice",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := comp.GreaterThan(tt.rowValue, tt.filterValue, tt.field, converter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestComparator_LessThan(t *testing.T) {
	comp := NewComparator()
	converter := schema.NewConverter()

	tests := []struct {
		name        string
		field       schema.FieldDef
		rowValue    string
		filterValue string
		expected    bool
	}{
		{
			name:        "Integer less",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "100",
			filterValue: "200",
			expected:    true,
		},
		{
			name:        "Integer not less",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "200",
			filterValue: "100",
			expected:    false,
		},
		{
			name:        "Real less",
			field:       schema.FieldDef{Name: "balance", Type: "REAL"},
			rowValue:    "123.40",
			filterValue: "123.45",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := comp.LessThan(tt.rowValue, tt.filterValue, tt.field, converter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestComparator_GreaterThanOrEqual(t *testing.T) {
	comp := NewComparator()
	converter := schema.NewConverter()

	tests := []struct {
		name        string
		field       schema.FieldDef
		rowValue    string
		filterValue string
		expected    bool
	}{
		{
			name:        "Greater than",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "200",
			filterValue: "100",
			expected:    true,
		},
		{
			name:        "Equal",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "100",
			filterValue: "100",
			expected:    true,
		},
		{
			name:        "Less than",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "50",
			filterValue: "100",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := comp.GreaterThanOrEqual(tt.rowValue, tt.filterValue, tt.field, converter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestComparator_LessThanOrEqual(t *testing.T) {
	comp := NewComparator()
	converter := schema.NewConverter()

	tests := []struct {
		name        string
		field       schema.FieldDef
		rowValue    string
		filterValue string
		expected    bool
	}{
		{
			name:        "Less than",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "50",
			filterValue: "100",
			expected:    true,
		},
		{
			name:        "Equal",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "100",
			filterValue: "100",
			expected:    true,
		},
		{
			name:        "Greater than",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "200",
			filterValue: "100",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := comp.LessThanOrEqual(tt.rowValue, tt.filterValue, tt.field, converter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestComparator_In(t *testing.T) {
	comp := NewComparator()
	converter := schema.NewConverter()

	tests := []struct {
		name        string
		field       schema.FieldDef
		rowValue    string
		filterValue string
		expected    bool
	}{
		{
			name:        "Integer in list",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "2",
			filterValue: "1,2,3",
			expected:    true,
		},
		{
			name:        "Integer not in list",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER"},
			rowValue:    "5",
			filterValue: "1,2,3",
			expected:    false,
		},
		{
			name:        "Text in list",
			field:       schema.FieldDef{Name: "city", Type: "TEXT"},
			rowValue:    "Moscow",
			filterValue: "Moscow,SPb,Kazan",
			expected:    true,
		},
		{
			name:        "Text not in list",
			field:       schema.FieldDef{Name: "city", Type: "TEXT"},
			rowValue:    "London",
			filterValue: "Moscow,SPb,Kazan",
			expected:    false,
		},
		{
			name:        "Text in list with spaces",
			field:       schema.FieldDef{Name: "city", Type: "TEXT"},
			rowValue:    "Moscow",
			filterValue: "Moscow, SPb, Kazan",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := comp.In(tt.rowValue, tt.filterValue, tt.field, converter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestComparator_Between(t *testing.T) {
	comp := NewComparator()
	converter := schema.NewConverter()

	tests := []struct {
		name      string
		field     schema.FieldDef
		rowValue  string
		lowValue  string
		highValue string
		expected  bool
	}{
		{
			name:      "Integer in range",
			field:     schema.FieldDef{Name: "age", Type: "INTEGER"},
			rowValue:  "25",
			lowValue:  "18",
			highValue: "65",
			expected:  true,
		},
		{
			name:      "Integer at lower bound",
			field:     schema.FieldDef{Name: "age", Type: "INTEGER"},
			rowValue:  "18",
			lowValue:  "18",
			highValue: "65",
			expected:  true,
		},
		{
			name:      "Integer at upper bound",
			field:     schema.FieldDef{Name: "age", Type: "INTEGER"},
			rowValue:  "65",
			lowValue:  "18",
			highValue: "65",
			expected:  true,
		},
		{
			name:      "Integer below range",
			field:     schema.FieldDef{Name: "age", Type: "INTEGER"},
			rowValue:  "10",
			lowValue:  "18",
			highValue: "65",
			expected:  false,
		},
		{
			name:      "Integer above range",
			field:     schema.FieldDef{Name: "age", Type: "INTEGER"},
			rowValue:  "70",
			lowValue:  "18",
			highValue: "65",
			expected:  false,
		},
		{
			name:      "Real in range",
			field:     schema.FieldDef{Name: "balance", Type: "REAL"},
			rowValue:  "50.5",
			lowValue:  "10.0",
			highValue: "100.0",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := comp.Between(tt.rowValue, tt.lowValue, tt.highValue, tt.field, converter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestComparator_Like(t *testing.T) {
	comp := NewComparator()

	tests := []struct {
		name     string
		rowValue string
		pattern  string
		expected bool
	}{
		{
			name:     "Exact match",
			rowValue: "John",
			pattern:  "John",
			expected: true,
		},
		{
			name:     "Prefix wildcard",
			rowValue: "Johnson",
			pattern:  "John%",
			expected: true,
		},
		{
			name:     "Suffix wildcard",
			rowValue: "Johnson",
			pattern:  "%son",
			expected: true,
		},
		{
			name:     "Both wildcards",
			rowValue: "Johnson",
			pattern:  "%oh%",
			expected: true,
		},
		{
			name:     "Single char wildcard",
			rowValue: "John",
			pattern:  "J_hn",
			expected: true,
		},
		{
			name:     "No match",
			rowValue: "John",
			pattern:  "Jane",
			expected: false,
		},
		{
			name:     "Multiple wildcards",
			rowValue: "test@example.com",
			pattern:  "%@%.com",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := comp.Like(tt.rowValue, tt.pattern)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v for pattern %s matching %s",
					tt.expected, result, tt.pattern, tt.rowValue)
			}
		})
	}
}

func TestComparator_Errors(t *testing.T) {
	comp := NewComparator()
	converter := schema.NewConverter()

	t.Run("Invalid integer", func(t *testing.T) {
		field := schema.FieldDef{Name: "id", Type: "INTEGER"}
		_, err := comp.GreaterThan("invalid", "123", field, converter)
		if err == nil {
			t.Error("expected error for invalid integer")
		}
	})

	t.Run("Invalid real", func(t *testing.T) {
		field := schema.FieldDef{Name: "balance", Type: "REAL"}
		_, err := comp.LessThan("invalid", "123.45", field, converter)
		if err == nil {
			t.Error("expected error for invalid real")
		}
	})
}
