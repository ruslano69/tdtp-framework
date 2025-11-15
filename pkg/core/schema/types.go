package schema

import (
	"fmt"
	"time"
)

// DataType представляет тип данных поля
type DataType string

// Поддерживаемые типы данных согласно спецификации TDTP
const (
	TypeInteger   DataType = "INTEGER"
	TypeInt       DataType = "INT"
	TypeReal      DataType = "REAL"
	TypeFloat     DataType = "FLOAT"
	TypeDouble    DataType = "DOUBLE"
	TypeDecimal   DataType = "DECIMAL"
	TypeText      DataType = "TEXT"
	TypeVarchar   DataType = "VARCHAR"
	TypeChar      DataType = "CHAR"
	TypeString    DataType = "STRING"
	TypeBoolean   DataType = "BOOLEAN"
	TypeBool      DataType = "BOOL"
	TypeDate      DataType = "DATE"
	TypeDatetime  DataType = "DATETIME"
	TypeTimestamp DataType = "TIMESTAMP"
	TypeBlob      DataType = "BLOB"
)

// TypedValue представляет типизированное значение
type TypedValue struct {
	Type      DataType
	RawValue  string
	IsNull    bool
	IntValue  *int64
	FloatValue *float64
	StringValue *string
	BoolValue  *bool
	TimeValue  *time.Time
	BlobValue  []byte
}

// FieldDef расширенное определение поля с валидацией
type FieldDef struct {
	Name      string
	Type      DataType
	Length    int
	Precision int
	Scale     int
	Timezone  string
	Key       bool
	Nullable  bool
}

// ValidationError ошибка валидации
type ValidationError struct {
	Field   string
	Message string
	Value   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s': %s (value: '%s')", 
		e.Field, e.Message, e.Value)
}

// IsNumericType проверяет является ли тип числовым
func IsNumericType(t DataType) bool {
	switch t {
	case TypeInteger, TypeInt, TypeReal, TypeFloat, TypeDouble, TypeDecimal:
		return true
	default:
		return false
	}
}

// IsTextType проверяет является ли тип текстовым
func IsTextType(t DataType) bool {
	switch t {
	case TypeText, TypeVarchar, TypeChar, TypeString:
		return true
	default:
		return false
	}
}

// IsDateTimeType проверяет является ли тип временным
func IsDateTimeType(t DataType) bool {
	switch t {
	case TypeDate, TypeDatetime, TypeTimestamp:
		return true
	default:
		return false
	}
}

// IsBooleanType проверяет является ли тип логическим
func IsBooleanType(t DataType) bool {
	return t == TypeBoolean || t == TypeBool
}

// IsBlobType проверяет является ли тип бинарным
func IsBlobType(t DataType) bool {
	return t == TypeBlob
}

// NormalizeType нормализует синонимы типов
func NormalizeType(t DataType) DataType {
	switch t {
	case TypeInt:
		return TypeInteger
	case TypeFloat, TypeDouble:
		return TypeReal
	case TypeVarchar, TypeChar, TypeString:
		return TypeText
	case TypeBool:
		return TypeBoolean
	default:
		return t
	}
}

// IsValidType проверяет валидность типа данных
func IsValidType(t DataType) bool {
	normalized := NormalizeType(t)
	switch normalized {
	case TypeInteger, TypeReal, TypeDecimal, TypeText, 
		 TypeBoolean, TypeDate, TypeDatetime, TypeTimestamp, TypeBlob:
		return true
	default:
		return false
	}
}

// GetDefaultPrecision возвращает точность по умолчанию для DECIMAL
func GetDefaultPrecision() int {
	return 18
}

// GetDefaultScale возвращает масштаб по умолчанию для DECIMAL
func GetDefaultScale() int {
	return 2
}

// GetDefaultTimezone возвращает таймзону по умолчанию
func GetDefaultTimezone() string {
	return "UTC"
}
