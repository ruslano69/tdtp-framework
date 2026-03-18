package packet

import "strings"

// Canonical SpecialValues markers per TDTP spec v1.3.1.
// These are fixed by the specification and must not be changed.
const (
	SpecNullMarker   = "[NULL]"
	SpecNaNMarker    = "NaN"
	SpecInfMarker    = "INF"
	SpecNegInfMarker = "-INF"
	SpecNoDateMarker = "0000-00-00" // canonical "no date" / zero-date marker
)

// nullSentinel matches base.NullSentinel — both must stay "\x00".
// Redeclared here to avoid a circular import between packet ↔ base.
const nullSentinel = "\x00"

// rawInfinityForms covers all Infinity representations that DBValueToString may produce.
var rawInfinityForms = map[string]bool{
	"Inf": true, "+Inf": true, "Infinity": true, "+Infinity": true,
}

// rawNegInfinityForms covers all negative Infinity representations.
var rawNegInfinityForms = map[string]bool{
	"-Inf": true, "-Infinity": true,
}

// isFloatField reports whether a field type can carry NaN/Infinity.
// Checks against the normalized TDTP type names without importing the schema package
// (which already imports packet, so importing schema here would create a cycle).
func isFloatField(fieldType string) bool {
	switch strings.ToUpper(fieldType) {
	case "REAL", "FLOAT", "DOUBLE", "DECIMAL", "NUMERIC", "MONEY", "SMALLMONEY":
		return true
	}
	return false
}

// isDateField reports whether a field type can carry NoDate or date-Infinity.
func isDateField(fieldType string) bool {
	switch strings.ToUpper(fieldType) {
	case "DATE", "DATETIME", "TIMESTAMP", "DATETIME2", "DATETIMEOFFSET", "SMALLDATETIME":
		return true
	}
	return false
}

// DetectAndApply scans all rows, detects which SpecialValues actually appear per
// field, updates Schema.Fields accordingly (only for fields where specials were found),
// and re-encodes the affected cell values using canonical markers.
//
// Rules for float/decimal columns:
//   - nullSentinel (DB NULL)          → SpecialValues.Null    = "[NULL]"
//   - "NaN"                           → SpecialValues.NaN     = "NaN"
//   - "Inf"/"+Inf"/"Infinity"         → SpecialValues.Infinity = "INF"
//   - "-Inf"/"-Infinity"              → SpecialValues.NegInfinity = "-INF"
//
// Rules for date/datetime/timestamp columns:
//   - nullSentinel                    → SpecialValues.Null    = "[NULL]"
//   - "0000-00-00"                    → SpecialValues.NoDate  = "0000-00-00"
//   - "Infinity"/"+Inf" etc           → SpecialValues.Infinity = "INF"  (PostgreSQL date infinity)
//   - "-Infinity"/"-Inf" etc         → SpecialValues.NegInfinity = "-INF"
//
// The returned rows and schema are safe to use for packet generation.
// If no specials are found the function returns the inputs unchanged.
func DetectAndApply(rows [][]string, sch Schema) ([][]string, Schema) {
	if len(rows) == 0 || len(sch.Fields) == 0 {
		return rows, sch
	}

	cols := len(sch.Fields)

	// Phase 1: detect which specials appear in each column.
	type detected struct {
		hasNull   bool
		hasNaN    bool
		hasInf    bool
		hasNegInf bool
		hasNoDate bool
	}
	det := make([]detected, cols)

	for _, row := range rows {
		for i := 0; i < cols && i < len(row); i++ {
			v := row[i]
			if v == nullSentinel {
				det[i].hasNull = true
				continue
			}
			fieldType := sch.Fields[i].Type
			if isFloatField(fieldType) {
				if v == "NaN" {
					det[i].hasNaN = true
				} else if rawInfinityForms[v] {
					det[i].hasInf = true
				} else if rawNegInfinityForms[v] {
					det[i].hasNegInf = true
				}
			} else if isDateField(fieldType) {
				if v == SpecNoDateMarker {
					det[i].hasNoDate = true
				} else if rawInfinityForms[v] {
					det[i].hasInf = true // PostgreSQL date infinity
				} else if rawNegInfinityForms[v] {
					det[i].hasNegInf = true
				}
			}
		}
	}

	// Check if anything was detected at all (fast path: nothing to do).
	anyDetected := false
	for _, d := range det {
		if d.hasNull || d.hasNaN || d.hasInf || d.hasNegInf || d.hasNoDate {
			anyDetected = true
			break
		}
	}
	if !anyDetected {
		return rows, sch
	}

	// Phase 2: build updated schema fields.
	updatedFields := make([]Field, cols)
	copy(updatedFields, sch.Fields)
	for i, d := range det {
		if !d.hasNull && !d.hasNaN && !d.hasInf && !d.hasNegInf && !d.hasNoDate {
			continue
		}
		sv := &SpecialValues{}
		if d.hasNull {
			sv.Null = &MarkerValue{Marker: SpecNullMarker}
		}
		if d.hasNaN {
			sv.NaN = &MarkerValue{Marker: SpecNaNMarker}
		}
		if d.hasInf {
			sv.Infinity = &MarkerValue{Marker: SpecInfMarker}
		}
		if d.hasNegInf {
			sv.NegInfinity = &MarkerValue{Marker: SpecNegInfMarker}
		}
		if d.hasNoDate {
			sv.NoDate = &MarkerValue{Marker: SpecNoDateMarker}
		}
		updatedFields[i].SpecialValues = sv
	}
	updatedSchema := Schema{Fields: updatedFields}

	// Phase 3: re-encode rows using canonical markers.
	updatedRows := make([][]string, len(rows))
	for j, row := range rows {
		updatedRow := make([]string, len(row))
		copy(updatedRow, row)
		for i := 0; i < cols && i < len(row); i++ {
			d := det[i]
			v := row[i]
			switch {
			case v == nullSentinel:
				if d.hasNull {
					updatedRow[i] = SpecNullMarker
				} else {
					updatedRow[i] = "" // no SpecialValues: backward-compat empty string
				}
			case d.hasInf && rawInfinityForms[v]:
				updatedRow[i] = SpecInfMarker
			case d.hasNegInf && rawNegInfinityForms[v]:
				updatedRow[i] = SpecNegInfMarker
				// "NaN" and "0000-00-00" are already canonical markers — no rename needed
			}
		}
		updatedRows[j] = updatedRow
	}

	return updatedRows, updatedSchema
}
