package oracle

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// Oracle folds unquoted identifiers to upper case; quoted identifiers retain
// their spelling. Brackets are accepted for the CLI's cross-database syntax.
func identifier(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("empty Oracle identifier")
	}
	switch {
	case strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]"):
		name = strings.ReplaceAll(name[1:len(name)-1], "]]", "]")
	case strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`):
		name = strings.ReplaceAll(name[1:len(name)-1], `""`, `"`)
	default:
		name = strings.ToUpper(name)
	}
	if name == "" || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("invalid Oracle identifier")
	}
	return name, nil
}

func quote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (a *Adapter) objectName(name string) (owner, table string, err error) {
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return "", "", fmt.Errorf("invalid Oracle table name %q", name)
	}
	if len(parts) == 2 {
		owner, err = identifier(parts[0])
		if err != nil {
			return "", "", err
		}
		name = parts[1]
	} else {
		owner = a.owner
	}
	table, err = identifier(name)
	return owner, table, err
}

func (a *Adapter) quotedTable(name string) (string, error) {
	owner, table, err := a.objectName(name)
	if err != nil {
		return "", err
	}
	return quote(owner) + "." + quote(table), nil
}

// SQLGenerator leaves simple names unquoted, and quotes names with punctuation.
func generatorName(name string) string {
	name = tdtql.StripBrackets(name)
	parts := strings.Split(name, ".")
	for i, part := range parts {
		for _, r := range part {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				parts[i] = quote(part)
				break
			}
		}
	}
	return strings.Join(parts, ".")
}

func selectExpressions(schema packet.Schema, requested []string) []string {
	if len(requested) == 0 {
		requested = make([]string, len(schema.Fields))
		for i, field := range schema.Fields {
			requested[i] = field.Name
		}
	}
	result := make([]string, len(requested))
	for i, name := range requested {
		requestedName, err := identifier(name)
		if err != nil {
			continue
		}
		for _, field := range schema.Fields {
			if !strings.EqualFold(field.Name, requestedName) {
				continue
			}
			col := quote(field.Name)
			if strings.EqualFold(field.Type, "DECIMAL") || strings.EqualFold(field.Type, "INTEGER") {
				result[i] = "TO_CHAR(" + col + ", 'TM9', 'NLS_NUMERIC_CHARACTERS=''.,''') AS " + col
			} else {
				result[i] = col
			}
			break
		}
	}
	return result
}

var rowLimit = regexp.MustCompile(`(?i) LIMIT ([0-9]+)(?: OFFSET ([0-9]+))?(\)|$)`)

type sqlDialect struct{ adapter *Adapter }

func (d sqlDialect) AdaptSQL(standardSQL, tableName string, schema packet.Schema, query *packet.Query) string {
	sql := standardSQL
	if table, err := d.adapter.quotedTable(tableName); err == nil {
		oldFrom := " FROM " + generatorName(tableName)
		if from := strings.Index(sql, oldFrom); from >= 0 {
			if selectPos := strings.LastIndex(sql[:from], "SELECT "); selectPos >= 0 && len(schema.Fields) > 0 {
				var requested []string
				if query != nil {
					requested = query.Fields
				}
				sql = sql[:selectPos+len("SELECT ")] + strings.Join(selectExpressions(schema, requested), ", ") + sql[from:]
			}
			sql = strings.Replace(sql, oldFrom, " FROM "+table, 1)
		}
	}
	sql = rowLimit.ReplaceAllStringFunc(sql, func(match string) string {
		parts := rowLimit.FindStringSubmatch(match)
		limit, offset, suffix := parts[1], parts[2], parts[3]
		if offset != "" {
			if limit == fmt.Sprint(tdtql.OffsetOnlyLimit) {
				return " OFFSET " + offset + " ROWS" + suffix
			}
			return " OFFSET " + offset + " ROWS FETCH NEXT " + limit + " ROWS ONLY" + suffix
		}
		return " FETCH FIRST " + limit + " ROWS ONLY" + suffix
	})
	// Oracle permits a table alias after a subquery but not the AS keyword.
	return strings.Replace(sql, ") AS _tail", ") tdtp_tail", 1)
}
