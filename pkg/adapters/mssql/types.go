package mssql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
)

// Type mapping for MS SQL Server 2012+
//
// SQL Server Type    TDTP Type       Notes
// ────────────────────────────────────────────────────
// INT, BIGINT        INTEGER
// DECIMAL, NUMERIC   DECIMAL         precision, scale
// NVARCHAR           TEXT            Unicode (preferred)
// VARCHAR            TEXT            ASCII
// BIT                BOOLEAN         0/1
// DATE               DATE            SQL Server 2008+
// DATETIME2          TIMESTAMP       High precision (preferred)
// DATETIME           TIMESTAMP       Legacy (3.33ms precision)
// UNIQUEIDENTIFIER   TEXT(36)        UUID as string
// VARBINARY          BLOB
// XML                TEXT            XML as string
// MONEY              DECIMAL(19,4)   Fixed precision

// MSSQLToTDTP converts MS SQL Server type to TDTP type.
func MSSQLToTDTP(sqlType string, nullable bool) (packet.Field, error) {
	sqlType = strings.ToUpper(strings.TrimSpace(sqlType))

	// Parse type with parameters
	baseType, length, precision, scale := ParseMSSQLType(sqlType)

	field := packet.Field{}

	switch baseType {
	// Integer types
	case "TINYINT", "SMALLINT", "INT", "BIGINT":
		field.Type = string(schema.TypeInteger)
		// Store original SQL type as subtype for exact roundtrip
		field.Subtype = strings.ToLower(baseType)

	// Decimal types
	case "DECIMAL", "NUMERIC":
		field.Type = string(schema.TypeDecimal)
		if precision > 0 {
			field.Precision = precision
		} else {
			field.Precision = 18 // SQL Server default
		}
		if scale > 0 {
			field.Scale = scale
		} else {
			field.Scale = 0
		}

	// Money types (convert to DECIMAL)
	case "MONEY":
		field.Type = string(schema.TypeDecimal)
		field.Precision = 19
		field.Scale = 4
		field.Subtype = "money"

	case "SMALLMONEY":
		field.Type = string(schema.TypeDecimal)
		field.Precision = 10
		field.Scale = 4
		field.Subtype = "smallmoney"

	// Float types
	case "FLOAT", "REAL":
		field.Type = string(schema.TypeReal)
		field.Subtype = strings.ToLower(baseType)

	// String types (prefer NVARCHAR for Unicode)
	case "NVARCHAR":
		field.Type = string(schema.TypeText)
		if length > 0 {
			field.Length = length
		}
		field.Subtype = "nvarchar"

	case "VARCHAR":
		field.Type = string(schema.TypeText)
		if length > 0 {
			field.Length = length
		}
		field.Subtype = "varchar"

	case "NCHAR":
		field.Type = string(schema.TypeText)
		if length > 0 {
			field.Length = length
		}
		field.Subtype = "nchar"

	case "CHAR":
		field.Type = string(schema.TypeText)
		if length > 0 {
			field.Length = length
		}
		field.Subtype = "char"

	// Legacy text types (deprecated but still work)
	case "TEXT":
		field.Type = string(schema.TypeText)
		field.Subtype = "text" // Mark as legacy

	case "NTEXT":
		field.Type = string(schema.TypeText)
		field.Subtype = "ntext" // Mark as legacy

	// Boolean
	case "BIT":
		field.Type = string(schema.TypeBoolean)

	// Date/Time types
	case "DATE":
		field.Type = string(schema.TypeDate)

	case "TIME":
		field.Type = string(schema.TypeText)
		field.Subtype = "time"

	case "DATETIME2":
		field.Type = string(schema.TypeTimestamp)
		field.Subtype = "datetime2" // Preferred, high precision

	case "DATETIME":
		field.Type = string(schema.TypeTimestamp)
		field.Subtype = "datetime" // Legacy, 3.33ms precision

	case "SMALLDATETIME":
		field.Type = string(schema.TypeTimestamp)
		field.Subtype = "smalldatetime" // 1 minute precision

	case "DATETIMEOFFSET":
		field.Type = string(schema.TypeTimestamp)
		field.Subtype = "datetimeoffset" // With timezone

	// UUID
	case "UNIQUEIDENTIFIER":
		field.Type = string(schema.TypeText)
		field.Length = 36 // UUID string length
		field.Subtype = "uniqueidentifier"

	// XML (as text)
	case "XML":
		field.Type = string(schema.TypeText)
		field.Subtype = "xml"

	// Binary types
	case "VARBINARY":
		field.Type = string(schema.TypeBlob)
		if length > 0 {
			field.Length = length
		}
		field.Subtype = "varbinary"

	case "BINARY":
		field.Type = string(schema.TypeBlob)
		if length > 0 {
			field.Length = length
		}
		field.Subtype = "binary"

	case "IMAGE":
		field.Type = string(schema.TypeBlob)
		field.Subtype = "image" // Legacy

	// timestamp/rowversion - 8-byte auto-generated version counter (NOT a datetime!)
	// This is a READ-ONLY field that changes on every UPDATE
	// Exported as hex string (not base64), so use TEXT type
	case "TIMESTAMP", "ROWVERSION":
		field.Type = string(schema.TypeText)
		field.Length = 16 // Hex string length (8 bytes = 16 hex chars max)
		field.Subtype = "rowversion"

	// sql_variant - can store values of various data types
	// https://learn.microsoft.com/en-us/sql/t-sql/data-types/sql-variant-transact-sql
	case "SQL_VARIANT":
		field.Type = string(schema.TypeText)
		field.Length = 8000 // Maximum size
		field.Subtype = "sql_variant"

	default:
		// Unknown type - default to TEXT with reasonable length
		field.Type = string(schema.TypeText)
		field.Length = 255 // Default length for unknown types
		field.Subtype = strings.ToLower(baseType)
	}

	return field, nil
}

// TDTPToMSSQL converts TDTP field to MS SQL Server CREATE TABLE type.
// Uses SQL Server 2012+ compatible types.
func TDTPToMSSQL(field packet.Field) string {
	tdtpType := schema.DataType(field.Type)

	// Check subtype for exact roundtrip conversion
	subtype := strings.ToLower(field.Subtype)

	switch tdtpType {
	case schema.TypeInteger, schema.TypeInt:
		// Use subtype if available for exact type
		switch subtype {
		case "tinyint":
			return "TINYINT"
		case "smallint":
			return "SMALLINT"
		case "int":
			return "INT"
		case "bigint":
			return "BIGINT"
		default:
			return "BIGINT" // Safe default
		}

	case schema.TypeDecimal:
		// Check for money types
		if subtype == "money" {
			return "MONEY"
		}
		if subtype == "smallmoney" {
			return "SMALLMONEY"
		}

		precision := field.Precision
		scale := field.Scale
		if precision == 0 {
			precision = 18
		}
		if scale < 0 {
			scale = 2
		}
		return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)

	case schema.TypeReal, schema.TypeFloat, schema.TypeDouble:
		if subtype == "real" {
			return "REAL"
		}
		return "FLOAT"

	case schema.TypeText, schema.TypeVarchar, schema.TypeChar, schema.TypeString:
		// Check for special text types
		switch subtype {
		case "rowversion":
			// timestamp/rowversion cannot be created manually - it's auto-generated
			// Skip this field during table creation (it's read-only)
			return "ROWVERSION"
		case "sql_variant":
			return "SQL_VARIANT"
		case "time":
			return "TIME"
		case "uniqueidentifier":
			return "UNIQUEIDENTIFIER"
		case "xml":
			return "XML"
		case "text":
			// Legacy type, but use NVARCHAR(MAX) instead
			return "NVARCHAR(MAX)"
		case "ntext":
			// Legacy type, but use NVARCHAR(MAX) instead
			return "NVARCHAR(MAX)"
		case "varchar":
			// ASCII string
			if field.Length > 0 && field.Length <= 8000 {
				return fmt.Sprintf("VARCHAR(%d)", field.Length)
			}
			return "VARCHAR(MAX)"
		case "char":
			// Fixed-length ASCII
			if field.Length > 0 && field.Length <= 8000 {
				return fmt.Sprintf("CHAR(%d)", field.Length)
			}
			return "CHAR(255)" // Reasonable default
		case "nchar":
			// Fixed-length Unicode
			if field.Length > 0 && field.Length <= 4000 {
				return fmt.Sprintf("NCHAR(%d)", field.Length)
			}
			return "NCHAR(255)"
		default:
			// Default to NVARCHAR for Unicode support
			if field.Length > 0 && field.Length <= 4000 {
				return fmt.Sprintf("NVARCHAR(%d)", field.Length)
			}
			return "NVARCHAR(MAX)"
		}

	case schema.TypeBoolean, schema.TypeBool:
		return "BIT"

	case schema.TypeDate:
		return "DATE"

	case schema.TypeDatetime, schema.TypeTimestamp:
		// Prefer DATETIME2 for new tables (SQL Server 2008+)
		switch subtype {
		case "datetime":
			return "DATETIME"
		case "smalldatetime":
			return "SMALLDATETIME"
		case "datetimeoffset":
			return "DATETIMEOFFSET"
		default:
			return "DATETIME2" // Best precision
		}

	case schema.TypeBlob:
		switch subtype {
		case "binary":
			if field.Length > 0 && field.Length <= 8000 {
				return fmt.Sprintf("BINARY(%d)", field.Length)
			}
			return "BINARY(255)"
		case "image":
			// Legacy, use VARBINARY(MAX) instead
			return "VARBINARY(MAX)"
		default:
			if field.Length > 0 && field.Length <= 8000 {
				return fmt.Sprintf("VARBINARY(%d)", field.Length)
			}
			return "VARBINARY(MAX)"
		}

	default:
		// Unknown type - default to NVARCHAR(MAX)
		return "NVARCHAR(MAX)"
	}
}

// ParseMSSQLType parses MS SQL Server type and extracts parameters.
// Examples:
//   - "INT" → ("INT", 0, 0, 0)
//   - "NVARCHAR(100)" → ("NVARCHAR", 100, 0, 0)
//   - "DECIMAL(18,2)" → ("DECIMAL", 0, 18, 2)
//   - "VARBINARY(MAX)" → ("VARBINARY", -1, 0, 0) // MAX = -1
func ParseMSSQLType(sqlType string) (baseType string, length, precision, scale int) {
	sqlType = strings.ToUpper(strings.TrimSpace(sqlType))

	// Extract base type
	baseType = extractBaseType(sqlType)

	// Extract parameters from parentheses
	if idx := strings.Index(sqlType, "("); idx != -1 {
		paramsStr := strings.TrimSuffix(sqlType[idx+1:], ")")

		// Check for MAX
		if strings.ToUpper(paramsStr) == "MAX" {
			length = -1 // Indicate MAX
			return
		}

		// Check for comma (DECIMAL/NUMERIC types)
		if strings.Contains(paramsStr, ",") {
			parts := strings.Split(paramsStr, ",")
			if len(parts) == 2 {
				precision, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
				scale, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
			}
		} else {
			// Single parameter (length)
			length, _ = strconv.Atoi(strings.TrimSpace(paramsStr))
		}
	}

	return
}

// extractBaseType extracts base type from MS SQL Server type string.
func extractBaseType(sqlType string) string {
	// Remove parentheses and everything after
	if idx := strings.Index(sqlType, "("); idx != -1 {
		sqlType = sqlType[:idx]
	}
	return strings.TrimSpace(sqlType)
}

// BuildFieldFromColumn builds TDTP Field from MS SQL Server column info.
// This is used when reading schema from INFORMATION_SCHEMA.
func BuildFieldFromColumn(columnName, dataType string, length, precision, scale int, isPrimaryKey bool) packet.Field {
	field := packet.Field{
		Name: columnName,
		Key:  isPrimaryKey,
	}

	// Build type string with parameters
	var fullType string
	if length > 0 {
		if length == -1 {
			fullType = fmt.Sprintf("%s(MAX)", dataType)
		} else {
			fullType = fmt.Sprintf("%s(%d)", dataType, length)
		}
	} else if precision > 0 {
		if scale > 0 {
			fullType = fmt.Sprintf("%s(%d,%d)", dataType, precision, scale)
		} else {
			fullType = fmt.Sprintf("%s(%d)", dataType, precision)
		}
	} else {
		fullType = dataType
	}

	// Convert to TDTP
	tdtpField, _ := MSSQLToTDTP(fullType, false)
	field.Type = tdtpField.Type
	field.Length = tdtpField.Length
	field.Precision = tdtpField.Precision
	field.Scale = tdtpField.Scale
	field.Subtype = tdtpField.Subtype

	return field
}

// Common SQL Server type constants for reference
const (
	// Preferred types for new tables (SQL Server 2012+)
	TypeInteger   = "BIGINT"           // Use BIGINT for safety
	TypeDecimal   = "DECIMAL(18,2)"    // Default precision
	TypeText      = "NVARCHAR(MAX)"    // Unicode support
	TypeBoolean   = "BIT"              // 0/1
	TypeDate      = "DATE"             // SQL Server 2008+
	TypeTimestamp = "DATETIME2"        // High precision
	TypeUUID      = "UNIQUEIDENTIFIER" // GUID
	TypeBlob      = "VARBINARY(MAX)"   // Binary data
	TypeXML       = "XML"              // XML documents

	// Legacy types (avoid for new tables, but support for compatibility)
	TypeTextLegacy     = "TEXT"     // Use NVARCHAR(MAX) instead
	TypeNTextLegacy    = "NTEXT"    // Use NVARCHAR(MAX) instead
	TypeImageLegacy    = "IMAGE"    // Use VARBINARY(MAX) instead
	TypeDateTimeLegacy = "DATETIME" // Use DATETIME2 instead
)
