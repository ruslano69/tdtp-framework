package cliquery_test

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/cliquery"
)

// ─────────────────────────────────────────────────────────────────
// BuildQuery – nil when no flags provided
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_NilWhenNoFlags(t *testing.T) {
	q, err := cliquery.BuildQuery(nil, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q != nil {
		t.Errorf("expected nil query when no flags provided, got %+v", q)
	}
}

// ─────────────────────────────────────────────────────────────────
// --where: single flag, simple operators
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_SingleWhere_Eq(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"status = active"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q == nil || q.Filters == nil || q.Filters.And == nil {
		t.Fatal("expected non-nil query with AND filters")
	}
	if len(q.Filters.And.Filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(q.Filters.And.Filters))
	}
	f := q.Filters.And.Filters[0]
	if f.Field != "status" {
		t.Errorf("field: want %q, got %q", "status", f.Field)
	}
	if f.Operator != "eq" {
		t.Errorf("operator: want %q, got %q", "eq", f.Operator)
	}
	if f.Value != "active" {
		t.Errorf("value: want %q, got %q", "active", f.Value)
	}
}

func TestBuildQuery_SingleWhere_Gt(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"age > 18"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := q.Filters.And.Filters[0]
	if f.Operator != "gt" {
		t.Errorf("operator: want %q, got %q", "gt", f.Operator)
	}
	if f.Value != "18" {
		t.Errorf("value: want %q, got %q", "18", f.Value)
	}
}

func TestBuildQuery_SingleWhere_Ne(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"role != admin"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := q.Filters.And.Filters[0]
	if f.Operator != "ne" {
		t.Errorf("operator: want %q, got %q", "ne", f.Operator)
	}
}

func TestBuildQuery_SingleWhere_Gte(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"score >= 90"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := q.Filters.And.Filters[0]
	if f.Operator != "gte" {
		t.Errorf("operator: want %q, got %q", "gte", f.Operator)
	}
}

func TestBuildQuery_SingleWhere_Lte(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"score <= 50"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := q.Filters.And.Filters[0]
	if f.Operator != "lte" {
		t.Errorf("operator: want %q, got %q", "lte", f.Operator)
	}
}

// ─────────────────────────────────────────────────────────────────
// --where: IN (...) operator (v1.7.1 feature)
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_Where_IN_Integers(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"role IN (1,2,3)"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := q.Filters.And.Filters[0]
	if f.Operator != "in" {
		t.Errorf("operator: want %q, got %q", "in", f.Operator)
	}
	if f.Value != "1,2,3" {
		t.Errorf("value: want %q, got %q", "1,2,3", f.Value)
	}
}

func TestBuildQuery_Where_IN_Strings(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"status IN ('active','pending')"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := q.Filters.And.Filters[0]
	if f.Operator != "in" {
		t.Errorf("operator: want %q, got %q", "in", f.Operator)
	}
	// Quotes should be stripped: active,pending
	if f.Value != "active,pending" {
		t.Errorf("value: want %q, got %q", "active,pending", f.Value)
	}
}

func TestBuildQuery_Where_IN_Spaces(t *testing.T) {
	// Extra spaces inside IN list should be handled
	q, err := cliquery.BuildQuery([]string{"dept_id IN (10, 20, 30)"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := q.Filters.And.Filters[0]
	if f.Operator != "in" {
		t.Errorf("operator: want %q, got %q", "in", f.Operator)
	}
	if f.Value != "10,20,30" {
		t.Errorf("value: want %q, got %q", "10,20,30", f.Value)
	}
}

// ─────────────────────────────────────────────────────────────────
// --where: repeatable flags combined with AND (v1.7.1 feature)
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_MultipleWhere_CombinedWithAND(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"age > 18", "status = active"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Filters == nil || q.Filters.And == nil {
		t.Fatal("expected AND filters")
	}
	// Two separate --where flags → two filters in the AND group
	if len(q.Filters.And.Filters) != 2 {
		t.Errorf("expected 2 filters (one per --where flag), got %d", len(q.Filters.And.Filters))
	}

	fields := map[string]string{
		q.Filters.And.Filters[0].Field: q.Filters.And.Filters[0].Operator,
		q.Filters.And.Filters[1].Field: q.Filters.And.Filters[1].Operator,
	}
	if fields["age"] != "gt" {
		t.Errorf("age filter: want operator %q, got %q", "gt", fields["age"])
	}
	if fields["status"] != "eq" {
		t.Errorf("status filter: want operator %q, got %q", "eq", fields["status"])
	}
}

func TestBuildQuery_MultipleWhere_ThreeFlags(t *testing.T) {
	q, err := cliquery.BuildQuery(
		[]string{"age > 18", "status = active", "role IN (1,2,3)"},
		"", 0, 0,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.Filters.And.Filters) != 3 {
		t.Errorf("expected 3 filters, got %d", len(q.Filters.And.Filters))
	}
}

func TestBuildQuery_MultipleWhere_FirstIsIN(t *testing.T) {
	// First --where flag uses IN, second uses simple equality
	q, err := cliquery.BuildQuery(
		[]string{"dept_id IN (10,20)", "active = true"},
		"", 0, 0,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.Filters.And.Filters) != 2 {
		t.Errorf("expected 2 filters, got %d", len(q.Filters.And.Filters))
	}
	// Find the IN filter
	var inFilter, eqFilter string
	for _, f := range q.Filters.And.Filters {
		if f.Operator == "in" {
			inFilter = f.Field
		}
		if f.Operator == "eq" {
			eqFilter = f.Field
		}
	}
	if inFilter != "dept_id" {
		t.Errorf("expected IN filter on dept_id, got %q", inFilter)
	}
	if eqFilter != "active" {
		t.Errorf("expected eq filter on active, got %q", eqFilter)
	}
}

// ─────────────────────────────────────────────────────────────────
// --where: BETWEEN
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_Where_BETWEEN(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"age BETWEEN 18 AND 65"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := q.Filters.And.Filters[0]
	if f.Operator != "between" {
		t.Errorf("operator: want %q, got %q", "between", f.Operator)
	}
	if f.Value != "18" {
		t.Errorf("value: want %q, got %q", "18", f.Value)
	}
	if f.Value2 != "65" {
		t.Errorf("value2: want %q, got %q", "65", f.Value2)
	}
}

// ─────────────────────────────────────────────────────────────────
// --where: IS NULL / IS NOT NULL
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_Where_IsNull(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"deleted_at IS NULL"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := q.Filters.And.Filters[0]
	if f.Operator != "is_null" {
		t.Errorf("operator: want %q, got %q", "is_null", f.Operator)
	}
	if f.Field != "deleted_at" {
		t.Errorf("field: want %q, got %q", "deleted_at", f.Field)
	}
}

func TestBuildQuery_Where_IsNotNull(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"email IS NOT NULL"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := q.Filters.And.Filters[0]
	if f.Operator != "is_not_null" {
		t.Errorf("operator: want %q, got %q", "is_not_null", f.Operator)
	}
}

// ─────────────────────────────────────────────────────────────────
// --where: AND / OR inside a single clause
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_Where_InternalAND(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"age > 18 AND status = active"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Filters.And == nil || len(q.Filters.And.Filters) != 2 {
		t.Errorf("expected 2 AND filters, got %v", q.Filters)
	}
}

func TestBuildQuery_Where_InternalOR(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"status = active OR status = pending"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Filters.Or == nil {
		t.Fatal("expected OR filters")
	}
	if len(q.Filters.Or.Filters) != 2 {
		t.Errorf("expected 2 OR filters, got %d", len(q.Filters.Or.Filters))
	}
}

// ─────────────────────────────────────────────────────────────────
// --where: quoted values
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_Where_QuotedValue(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{`date eq "09.02.2026"`}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := q.Filters.And.Filters[0]
	// Quotes are stripped
	if f.Value != "09.02.2026" {
		t.Errorf("value: want %q, got %q", "09.02.2026", f.Value)
	}
}

// ─────────────────────────────────────────────────────────────────
// --where: empty / whitespace flags are skipped
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_Where_EmptyStringSkipped(t *testing.T) {
	q, err := cliquery.BuildQuery([]string{"", "  ", "status = active"}, "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.Filters.And.Filters) != 1 {
		t.Errorf("expected 1 filter (empty strings skipped), got %d", len(q.Filters.And.Filters))
	}
}

// ─────────────────────────────────────────────────────────────────
// --where: invalid clause returns error
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_Where_InvalidClause(t *testing.T) {
	_, err := cliquery.BuildQuery([]string{"thisIsNotACondition"}, "", 0, 0)
	if err == nil {
		t.Error("expected error for invalid WHERE clause, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────
// --order-by
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_OrderBy_SingleAsc(t *testing.T) {
	q, err := cliquery.BuildQuery(nil, "name ASC", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.OrderBy == nil {
		t.Fatal("expected OrderBy to be set")
	}
	if q.OrderBy.Field != "name" {
		t.Errorf("field: want %q, got %q", "name", q.OrderBy.Field)
	}
	if q.OrderBy.Direction != "ASC" {
		t.Errorf("direction: want ASC, got %q", q.OrderBy.Direction)
	}
}

func TestBuildQuery_OrderBy_SingleDesc(t *testing.T) {
	q, err := cliquery.BuildQuery(nil, "created_at DESC", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.OrderBy.Direction != "DESC" {
		t.Errorf("direction: want DESC, got %q", q.OrderBy.Direction)
	}
}

func TestBuildQuery_OrderBy_Multi(t *testing.T) {
	q, err := cliquery.BuildQuery(nil, "name ASC, age DESC", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.OrderBy.Fields) != 2 {
		t.Fatalf("expected 2 order fields, got %d", len(q.OrderBy.Fields))
	}
	if q.OrderBy.Fields[0].Name != "name" || q.OrderBy.Fields[0].Direction != "ASC" {
		t.Errorf("first field: want name ASC, got %+v", q.OrderBy.Fields[0])
	}
	if q.OrderBy.Fields[1].Name != "age" || q.OrderBy.Fields[1].Direction != "DESC" {
		t.Errorf("second field: want age DESC, got %+v", q.OrderBy.Fields[1])
	}
}

// ─────────────────────────────────────────────────────────────────
// --limit
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_Limit_Positive(t *testing.T) {
	q, err := cliquery.BuildQuery(nil, "", 100, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Limit != 100 {
		t.Errorf("limit: want 100, got %d", q.Limit)
	}
}

func TestBuildQuery_Limit_Negative_TailMode(t *testing.T) {
	// Negative limit means "last N rows" (tail mode)
	q, err := cliquery.BuildQuery(nil, "", -50, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Limit != -50 {
		t.Errorf("limit: want -50 (tail), got %d", q.Limit)
	}
}

// ─────────────────────────────────────────────────────────────────
// --offset
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_Offset(t *testing.T) {
	q, err := cliquery.BuildQuery(nil, "", 0, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Offset != 200 {
		t.Errorf("offset: want 200, got %d", q.Offset)
	}
}

// ─────────────────────────────────────────────────────────────────
// --fields (SplitCommaSeparated — column projection, v1.7.1 feature)
// ─────────────────────────────────────────────────────────────────

func TestSplitCommaSeparated_EmptyString(t *testing.T) {
	result := cliquery.SplitCommaSeparated("")
	if result != nil {
		t.Errorf("expected nil for empty string, got %v", result)
	}
}

func TestSplitCommaSeparated_SingleField(t *testing.T) {
	result := cliquery.SplitCommaSeparated("id")
	if len(result) != 1 || result[0] != "id" {
		t.Errorf("expected [id], got %v", result)
	}
}

func TestSplitCommaSeparated_MultipleFields(t *testing.T) {
	result := cliquery.SplitCommaSeparated("id,email,status")
	if len(result) != 3 {
		t.Fatalf("expected 3 fields, got %d: %v", len(result), result)
	}
	want := []string{"id", "email", "status"}
	for i, w := range want {
		if result[i] != w {
			t.Errorf("field[%d]: want %q, got %q", i, w, result[i])
		}
	}
}

func TestSplitCommaSeparated_TrimsSpaces(t *testing.T) {
	result := cliquery.SplitCommaSeparated("id, email , status")
	if len(result) != 3 {
		t.Fatalf("expected 3 fields, got %d: %v", len(result), result)
	}
	want := []string{"id", "email", "status"}
	for i, w := range want {
		if result[i] != w {
			t.Errorf("field[%d]: want %q, got %q (spaces not trimmed)", i, w, result[i])
		}
	}
}

func TestSplitCommaSeparated_DropsEmptyElements(t *testing.T) {
	// Trailing comma or double commas should not produce empty strings
	result := cliquery.SplitCommaSeparated("id,,email,")
	if len(result) != 2 {
		t.Errorf("expected 2 elements (empty dropped), got %d: %v", len(result), result)
	}
}

func TestSplitCommaSeparated_TypicalFieldsProjection(t *testing.T) {
	// Typical --fields usage: column projection
	result := cliquery.SplitCommaSeparated("user_id,first_name,last_name,email,created_at")
	if len(result) != 5 {
		t.Errorf("expected 5 fields, got %d: %v", len(result), result)
	}
}

// ─────────────────────────────────────────────────────────────────
// Combined: --where + --limit + --offset
// ─────────────────────────────────────────────────────────────────

func TestBuildQuery_WhereAndLimitAndOffset(t *testing.T) {
	q, err := cliquery.BuildQuery(
		[]string{"status = active", "role IN (1,2)"},
		"name ASC",
		100, 50,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Limit != 100 {
		t.Errorf("limit: want 100, got %d", q.Limit)
	}
	if q.Offset != 50 {
		t.Errorf("offset: want 50, got %d", q.Offset)
	}
	if q.OrderBy == nil || q.OrderBy.Field != "name" {
		t.Errorf("expected ORDER BY name, got %+v", q.OrderBy)
	}
	if len(q.Filters.And.Filters) != 2 {
		t.Errorf("expected 2 WHERE filters, got %d", len(q.Filters.And.Filters))
	}
}

// ─────────────────────────────────────────────────────────────────
// ParseWhereClause standalone tests
// ─────────────────────────────────────────────────────────────────

func TestParseWhereClause_LIKE(t *testing.T) {
	f, err := cliquery.ParseWhereClause("name LIKE %smith%")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.And == nil || len(f.And.Filters) != 1 {
		t.Fatal("expected single AND filter")
	}
	if f.And.Filters[0].Operator != "like" {
		t.Errorf("operator: want %q, got %q", "like", f.And.Filters[0].Operator)
	}
}
