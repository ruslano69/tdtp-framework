package base

import (
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func TestMSSQLAdapter_AdaptSQL_ANSIQuotedTableName(t *testing.T) {
	// Regression test: table names with special chars ($ space) are ANSI-quoted by
	// GenerateSQL ("ZTR$Timesheet Line"). MSSQLAdapter must replace the full ANSI-quoted
	// token, not the bare substring, to avoid producing invalid SQL like
	// "[dbo].["ZTR$Timesheet Line"]" which causes a full table scan (17 GB RAM).
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "Entry No_"},
			{Name: "Calendar Date"},
		},
	}

	// GenerateSQL produces ANSI-quoted table: "ZTR$Timesheet Line"
	standardSQL := `SELECT * FROM "ZTR$Timesheet Line" WHERE "Calendar Date" >= '2024-08-12T00:00:00Z'`
	got := adapter.AdaptSQL(standardSQL, "ZTR$Timesheet Line", schema, nil)

	if strings.Contains(got, `"`) {
		t.Errorf("AdaptSQL left ANSI double-quotes in output: %s", got)
	}
	if !strings.Contains(got, "[dbo].[ZTR$Timesheet Line]") {
		t.Errorf("AdaptSQL did not produce [dbo].[ZTR$Timesheet Line]: %s", got)
	}
	if !strings.Contains(got, "[Calendar Date]") {
		t.Errorf("AdaptSQL did not bracket-quote field name: %s", got)
	}
}

func TestMSSQLAdapter_AdaptSQL_SafeTableName(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{Fields: []packet.Field{{Name: "id"}, {Name: "name"}}}

	// Safe table name is NOT ANSI-quoted by GenerateSQL
	standardSQL := `SELECT * FROM Users WHERE id = 1`
	got := adapter.AdaptSQL(standardSQL, "Users", schema, nil)

	if !strings.Contains(got, "[dbo].[Users]") {
		t.Errorf("AdaptSQL did not produce [dbo].[Users]: %s", got)
	}
}

// SQL Server datetime не принимает суффикс 'Z' (UTC marker) у ISO 8601 литералов:
// '2024-08-12T00:00:00Z' → "Conversion failed when converting date/time from character string".
// Падение SQL триггерит fallback на full table scan. Strip 'Z' страхует pushdown.
func TestMSSQLAdapter_AdaptSQL_StripsISO8601ZSuffix(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{Fields: []packet.Field{{Name: "Calendar Date"}}}

	standardSQL := `SELECT * FROM Timesheet WHERE "Calendar Date" >= '2024-08-12T00:00:00Z' AND "Calendar Date" <= '2026-05-19T23:59:59Z'`
	got := adapter.AdaptSQL(standardSQL, "Timesheet", schema, nil)

	if strings.Contains(got, "Z'") {
		t.Errorf("AdaptSQL did not strip 'Z' suffix from datetime literals: %s", got)
	}
	if !strings.Contains(got, "'2024-08-12T00:00:00'") {
		t.Errorf("AdaptSQL must keep ISO 8601 datetime body intact: %s", got)
	}
	if !strings.Contains(got, "'2026-05-19T23:59:59'") {
		t.Errorf("AdaptSQL must process every Z-suffixed literal: %s", got)
	}
}

// Без 'Z' literal остаётся неизменным — regex не должен трогать обычные строки.
func TestMSSQLAdapter_AdaptSQL_LeavesNonDatetimeStringsAlone(t *testing.T) {
	adapter := NewMSSQLAdapter("dbo")
	schema := packet.Schema{Fields: []packet.Field{{Name: "Code"}}}

	// Строка с 'Z' но не ISO 8601 — должна остаться нетронутой
	standardSQL := `SELECT * FROM Users WHERE "Code" = 'XYZ' AND "Code" = '24626-1'`
	got := adapter.AdaptSQL(standardSQL, "Users", schema, nil)

	if !strings.Contains(got, "'XYZ'") {
		t.Errorf("regex stripped non-datetime 'XYZ' value: %s", got)
	}
	if !strings.Contains(got, "'24626-1'") {
		t.Errorf("regex damaged code-style value '24626-1': %s", got)
	}
}
