package xlsx

import (
	"math"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// ── typedValueToExcel ────────────────────────────────────────────────────────

func TestTypedValueToExcel_BigInt(t *testing.T) {
	// Values within 15 significant digits → numeric (no forced string)
	small := int64(999_999_999_999_999)
	tv := &schema.TypedValue{IntValue: &small}
	val, forceStr := typedValueToExcel(tv, schema.TypeInteger)
	if forceStr {
		t.Fatal("expected numeric, got forceStr for small int")
	}
	if val.(int64) != small {
		t.Fatalf("expected %d, got %v", small, val)
	}

	// Values exceeding 15 significant digits → string (preserve all digits)
	big := int64(1_234_567_890_123_456_789) // 19 digits
	tv2 := &schema.TypedValue{IntValue: &big}
	val2, forceStr2 := typedValueToExcel(tv2, schema.TypeInteger)
	if !forceStr2 {
		t.Fatal("expected forceStr for BIGINT exceeding 15 digits")
	}
	if val2.(string) != "1234567890123456789" {
		t.Fatalf("expected string '1234567890123456789', got %v", val2)
	}

	// Negative BIGINT
	neg := int64(-1_000_000_000_000_001)
	tv3 := &schema.TypedValue{IntValue: &neg}
	val3, forceStr3 := typedValueToExcel(tv3, schema.TypeInteger)
	if !forceStr3 {
		t.Fatal("expected forceStr for negative BIGINT")
	}
	if val3.(string) != "-1000000000000001" {
		t.Fatalf("unexpected value: %v", val3)
	}
}

func TestTypedValueToExcel_NaN(t *testing.T) {
	nan := math.NaN()
	tv := &schema.TypedValue{FloatValue: &nan}
	val, _ := typedValueToExcel(tv, schema.TypeReal)
	if val != nil {
		t.Fatalf("NaN should produce nil (blank cell), got %v", val)
	}
}

func TestTypedValueToExcel_Inf(t *testing.T) {
	posInf := math.Inf(1)
	tv := &schema.TypedValue{FloatValue: &posInf}
	val, _ := typedValueToExcel(tv, schema.TypeReal)
	if val != nil {
		t.Fatalf("+Inf should produce nil, got %v", val)
	}

	negInf := math.Inf(-1)
	tv2 := &schema.TypedValue{FloatValue: &negInf}
	val2, _ := typedValueToExcel(tv2, schema.TypeReal)
	if val2 != nil {
		t.Fatalf("-Inf should produce nil, got %v", val2)
	}
}

func TestTypedValueToExcel_Pre1900Date(t *testing.T) {
	ancient := time.Date(1899, 10, 12, 0, 0, 0, 0, time.UTC)
	tv := &schema.TypedValue{TimeValue: &ancient}

	val, forceStr := typedValueToExcel(tv, schema.TypeDate)
	if !forceStr {
		t.Fatal("pre-1900 date must be forceStr (text cell)")
	}
	if val.(string) != "1899-10-12" {
		t.Fatalf("expected '1899-10-12', got %v", val)
	}
}

func TestTypedValueToExcel_Post1900Date(t *testing.T) {
	d := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	tv := &schema.TypedValue{TimeValue: &d}

	val, forceStr := typedValueToExcel(tv, schema.TypeDate)
	if forceStr {
		t.Fatal("post-1900 date should NOT be forceStr (excelize handles it)")
	}
	got, ok := val.(time.Time)
	if !ok || !got.Equal(d) {
		t.Fatalf("expected time.Time %v, got %v", d, val)
	}
}

func TestTypedValueToExcel_NullMarker(t *testing.T) {
	s := packet.SpecNullMarker // "[NULL]"
	tv := &schema.TypedValue{StringValue: &s}
	val, _ := typedValueToExcel(tv, schema.TypeText)
	if val != nil {
		t.Fatalf("[NULL] marker must produce nil (blank cell), got %v", val)
	}
}

func TestTypedValueToExcel_StringForceStr(t *testing.T) {
	// Any string must use forceStr to prevent formula injection
	for _, s := range []string{"=SUM(A1)", "+1", "-1", "@foo", "hello"} {
		sv := s
		tv := &schema.TypedValue{StringValue: &sv}
		val, forceStr := typedValueToExcel(tv, schema.TypeText)
		if !forceStr {
			t.Errorf("string %q should be forceStr", s)
		}
		if val.(string) != s {
			t.Errorf("expected %q, got %v", s, val)
		}
	}
}

// ── excelSerialToTime ────────────────────────────────────────────────────────

func TestExcelSerialToTime(t *testing.T) {
	cases := []struct {
		serial float64
		want   time.Time
		desc   string
	}{
		{1, time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), "serial 1 = Jan 1, 1900"},
		{2, time.Date(1900, 1, 2, 0, 0, 0, 0, time.UTC), "serial 2 = Jan 2, 1900"},
		{59, time.Date(1900, 2, 28, 0, 0, 0, 0, time.UTC), "serial 59 = Feb 28, 1900"},
		{60, time.Date(1900, 2, 28, 0, 0, 0, 0, time.UTC), "serial 60 phantom → Feb 28, 1900"},
		{61, time.Date(1900, 3, 1, 0, 0, 0, 0, time.UTC), "serial 61 = Mar 1, 1900"},
		{44927, time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), "serial 44927 = Jan 1, 2023"},
		{44928, time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC), "serial 44928 = Jan 2, 2023"},
		// Noon on Jan 1, 2023: serial 44927.5
		{44927.5, time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC), "44927.5 = noon Jan 1, 2023"},
	}

	for _, tc := range cases {
		got := excelSerialToTime(tc.serial)
		if !got.Equal(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.desc, got, tc.want)
		}
	}
}

// ── convertFromExcel ─────────────────────────────────────────────────────────

func TestConvertFromExcel_DateSerial(t *testing.T) {
	// Jan 1, 2023 = serial 44927
	got := convertFromExcel("44927", schema.TypeDate)
	if got != "2023-01-01" {
		t.Fatalf("expected '2023-01-01', got %q", got)
	}
}

func TestConvertFromExcel_DatetimeSerialWithFraction(t *testing.T) {
	// Noon on Jan 1, 2023 = 44927.5
	got := convertFromExcel("44927.5", schema.TypeDatetime)
	if got != "2023-01-01T12:00:00Z" {
		t.Fatalf("expected '2023-01-01T12:00:00Z', got %q", got)
	}
}

func TestConvertFromExcel_BooleanVariants(t *testing.T) {
	cases := map[string]string{
		"TRUE": "1", "true": "1", "1": "1",
		"FALSE": "0", "false": "0", "0": "0",
	}
	for input, want := range cases {
		got := convertFromExcel(input, schema.TypeBoolean)
		if got != want {
			t.Errorf("boolean %q: expected %q, got %q", input, want, got)
		}
	}
}

// ── isExcelError ─────────────────────────────────────────────────────────────

func TestIsExcelError(t *testing.T) {
	errors := []string{"#N/A", "#DIV/0!", "#NUM!", "#VALUE!", "#REF!", "#NAME?", "#NULL!"}
	for _, e := range errors {
		if !isExcelError(e) {
			t.Errorf("expected %q to be recognized as Excel error", e)
		}
	}
	for _, ok := range []string{"", "hello", "42", "2023-01-01"} {
		if isExcelError(ok) {
			t.Errorf("expected %q NOT to be Excel error", ok)
		}
	}
}
