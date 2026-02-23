package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// J_SerializeValue converts a raw Python value to its canonical TDTP wire string.
//
// This is the single source of truth for type serialization in all Python adapters
// (pandas, xlsx, arrow, etc.). Adapters must delegate here instead of reimplementing
// the conversion logic.
//
// tdtpType — TDTP type: "BLOB", "TIMESTAMP", "DATETIME", "JSON", "JSONB", …
// value    — raw value as a plain string:
//   - BLOB:             hex-encoded bytes, e.g. "deadbeef00"
//   - TIMESTAMP/DATETIME: any ISO-8601 / RFC3339 parseable string
//   - JSON/JSONB:       any valid JSON string (re-marshaled compact, no whitespace)
//   - other types:      returned as-is
//
// Returns {"value":"..."} on success or {"error":"..."} on failure.
// Caller must free result with J_FreeString.
//
//export J_SerializeValue
func J_SerializeValue(tdtpType *C.char, value *C.char) *C.char {
	rawType := strings.ToUpper(C.GoString(tdtpType))
	v := C.GoString(value)
	normalized := schema.NormalizeType(schema.DataType(rawType))

	switch {
	case normalized == schema.TypeBlob:
		// Python passes hex-encoded bytes; Go decodes and re-encodes as Base64.
		// Matches: base64.StdEncoding.EncodeToString used in all Go adapters.
		raw, err := hex.DecodeString(v)
		if err != nil {
			return jErr(fmt.Sprintf("BLOB: invalid hex input: %v", err))
		}
		return jSerOK(base64.StdEncoding.EncodeToString(raw))

	case normalized == schema.TypeDatetime || normalized == schema.TypeTimestamp:
		// Parse any ISO-8601-like input and normalise to UTC RFC3339.
		// Matches: v.UTC().Format(time.RFC3339) used in all Go adapters.
		parsed, err := serParseDateTime(v)
		if err != nil {
			return jErr(fmt.Sprintf("TIMESTAMP: cannot parse %q: %v", v, err))
		}
		return jSerOK(parsed.UTC().Format(time.RFC3339))

	case rawType == "JSON" || rawType == "JSONB":
		// Re-marshal compact: eliminates whitespace, normalises key order,
		// produces lowercase true/false.  Matches: json.Marshal in Go adapters.
		var obj any
		if err := json.Unmarshal([]byte(v), &obj); err != nil {
			return jErr(fmt.Sprintf("JSON: invalid input: %v", err))
		}
		out, _ := json.Marshal(obj)
		return jSerOK(string(out))

	default:
		return jSerOK(v)
	}
}

// serParseDateTime tries multiple datetime formats in priority order.
// Returns an error only when none of the formats match.
func serParseDateTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,          // 2006-01-02T15:04:05Z07:00  (tz-aware)
		"2006-01-02T15:04:05", // naive ISO-8601 (no timezone) — treated as UTC
		"2006-01-02 15:04:05", // SQLite / MSSQL without T separator
		"2006-01-02",          // date only
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("no matching format")
}

// jSerOK returns {"value":"..."} — canonical success response for J_SerializeValue.
func jSerOK(v string) *C.char {
	b, _ := json.Marshal(map[string]string{"value": v})
	return C.CString(string(b))
}
