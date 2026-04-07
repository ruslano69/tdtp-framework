package tdtql

import "strings"

// StripBrackets removes MSSQL/Access-style bracket quoting from a single identifier.
// Handles both simple and schema-qualified names:
//
//	"[ZTR$Employee]"    → "ZTR$Employee"
//	"[dbo].[Orders]"    → "dbo.Orders"
//	"Users"             → "Users"
func StripBrackets(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "[") && strings.HasSuffix(p, "]") {
			parts[i] = p[1 : len(p)-1]
		} else {
			parts[i] = p
		}
	}
	return strings.Join(parts, ".")
}

// SplitFieldList splits a comma-separated field list with optional bracket-quoted names.
// Bracket-quoted names may contain spaces or commas:
//
//	"id,email,status"             → ["id", "email", "status"]
//	"id, [Birth Date], status"    → ["id", "Birth Date", "status"]
//	"[First, Last],[Birth Date]"  → ["First, Last", "Birth Date"]
//
// Used by --fields, --key-fields, --ignore-fields, --fixed-fields.
func SplitFieldList(s string) []string {
	var result []string
	n := len(s)
	for i := 0; i < n; {
		if s[i] == ' ' || s[i] == '\t' || s[i] == ',' {
			i++
			continue
		}
		start := i
		if s[i] == '[' {
			i++ // skip '['
			start = i
			for i < n && s[i] != ']' {
				i++
			}
			result = append(result, s[start:i])
			if i < n {
				i++ // skip ']'
			}
		} else {
			for i < n && s[i] != ',' {
				i++
			}
			if token := strings.TrimSpace(s[start:i]); token != "" {
				result = append(result, token)
			}
		}
	}
	return result
}
