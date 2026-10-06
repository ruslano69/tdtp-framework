package oracle

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func fieldFromColumn(name, nativeType string, length, precision, scale int, key, readOnly bool) packet.Field {
	typ := strings.ToUpper(nativeType)
	f := packet.Field{Name: name, Key: key, ReadOnly: readOnly}
	switch {
	case typ == "NUMBER":
		f.Precision, f.Scale = precision, scale
		switch {
		case precision > 0 && precision <= 18 && scale == 0:
			f.Type = "INTEGER"
		case precision == 19 && scale == 0:
			// NUMBER(19,0) is what this adapter creates for a TDTP INTEGER,
			// but a native one can hold values above int64. DECIMAL keeps
			// those exact; "bigint" lets an importer create BIGINT, so an
			// INTEGER no longer turns into NUMERIC(19,…) after a pass
			// through Oracle (packet.BigintDecimal).
			f.Type = "DECIMAL"
			f.Subtype = "bigint"
		default:
			f.Type = "DECIMAL"
		}
	case typ == "FLOAT", typ == "BINARY_FLOAT":
		f.Type = "REAL"
	case typ == "BINARY_DOUBLE":
		f.Type = "DOUBLE"
	case typ == "VARCHAR2", typ == "NVARCHAR2":
		f.Type, f.Length = "VARCHAR", length
	case typ == "CHAR", typ == "NCHAR":
		f.Type, f.Length = "CHAR", length
	case typ == "CLOB", typ == "NCLOB", typ == "LONG", typ == "XMLTYPE":
		f.Type = "TEXT"
	case typ == "BLOB", typ == "RAW", typ == "LONG RAW":
		f.Type = "BLOB"
	case typ == "DATE":
		// Oracle DATE stores hours/minutes/seconds, unlike TDTP DATE.
		f.Type = "DATETIME"
	case strings.HasPrefix(typ, "TIMESTAMP"):
		f.Type = "DATETIME"
		if strings.Contains(typ, "WITH TIME ZONE") || strings.Contains(typ, "WITH LOCAL TIME ZONE") {
			f.Type, f.Timezone = "TIMESTAMP", "UTC"
		}
	default:
		// Refuse unknown native types instead of silently turning them into TEXT.
		f.Type = ""
	}
	return f
}

func typeForField(f packet.Field) (string, error) {
	switch strings.ToUpper(f.Type) {
	case "INTEGER", "INT":
		return "NUMBER(19,0)", nil
	case "DECIMAL":
		if f.Precision == 0 {
			return "NUMBER", nil
		}
		if f.Precision < 1 || f.Precision > 38 || f.Scale < 0 || f.Scale > f.Precision {
			return "", fmt.Errorf("invalid Oracle NUMBER(%d,%d)", f.Precision, f.Scale)
		}
		return fmt.Sprintf("NUMBER(%d,%d)", f.Precision, f.Scale), nil
	case "REAL", "FLOAT":
		// BINARY_DOUBLE, not BINARY_FLOAT: TDTP REAL is a 64-bit double —
		// SQLite REAL and PostgreSQL float8 both arrive as REAL — and the
		// 32-bit type kept ~7 digits: 1234.56789012345 came back 1234.5679.
		return "BINARY_DOUBLE", nil
	case "DOUBLE":
		return "BINARY_DOUBLE", nil
	case "BOOLEAN", "BOOL":
		return "NUMBER(1,0)", nil
	case "CHAR":
		if f.Length >= 1 && f.Length <= 2000 {
			return fmt.Sprintf("CHAR(%d CHAR)", f.Length), nil
		}
		return "CLOB", nil
	case "VARCHAR", "STRING", "TEXT":
		if f.Length >= 1 && f.Length <= 4000 {
			return fmt.Sprintf("VARCHAR2(%d CHAR)", f.Length), nil
		}
		if f.Key {
			// A key cannot be a LOB (ORA-02329), and lengthless text keys are
			// ordinary: SQLite TEXT PRIMARY KEY, PostgreSQL text. 255 CHAR, as
			// the MySQL adapter defaults to, keeps a composite of several such
			// columns inside Oracle's index-key size; a longer value fails the
			// insert loudly (ORA-12899) rather than being cut.
			return "VARCHAR2(255 CHAR)", nil
		}
		return "CLOB", nil
	case "DATE":
		return "DATE", nil
	case "DATETIME":
		return "TIMESTAMP(6)", nil
	case "TIMESTAMP":
		return "TIMESTAMP(6) WITH TIME ZONE", nil
	case "BLOB":
		return "BLOB", nil
	default:
		return "", fmt.Errorf("unsupported TDTP type %q for Oracle", f.Type)
	}
}
