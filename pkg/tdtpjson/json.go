// Package tdtpjson converts TDTP packets to JSON arrays of objects.
//
// One object per row, keys from the Schema field names (not positional
// pipe strings) — the consumer addresses columns by name, like an API.
// Values are honestly typed via schema.Converter: integers and floats as
// JSON numbers, booleans as bools, NULLs as null, dates as ISO strings,
// everything else as strings. Integers past the JS safe range (2^53-1)
// go out as strings so downstream jq/node does not silently round them;
// NaN/±Inf (no JSON representation) become null.
//
// Rows stream through a single pass — no giant slice is ever built — so
// 100 MB pages convert in constant memory. Column order follows the
// schema (or --fields order); key order in JSON is therefore stable.
package tdtpjson

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// maxSafeJSInt is 2^53-1: the largest integer JavaScript represents
// exactly. Larger magnitudes are emitted as strings.
const maxSafeJSInt = int64(1<<53 - 1)

// Write converts pkt to a JSON array on w, applying query (WHERE,
// ORDER BY, LIMIT, OFFSET, fields projection) exactly like the CSV
// converter does. It returns the number of data objects written.
// pretty indents one object per line for humans; compact is the default
// for pipelines.
//
// The packet must hold plain row-major rows: callers decompress and
// expand compact/columnar layouts first (see WritePacket), or pass an
// already-plain packet. Encrypted content is refused.
func Write(ctx context.Context, w io.Writer, pkt *packet.DataPacket, query *packet.Query, pretty bool) (int, error) {
	_ = ctx
	if pkt.Data.Encryption != "" {
		return 0, fmt.Errorf("refusing to render encrypted content as JSON: decrypt first")
	}

	rows := pkt.GetRows()
	fields := pkt.Schema.Fields

	if query != nil {
		executor := tdtql.NewExecutor()
		execResult, err := executor.Execute(query, rows, pkt.Schema)
		if err != nil {
			return 0, fmt.Errorf("failed to apply query filters: %w", err)
		}
		rows = execResult.FilteredRows
		if len(query.Fields) > 0 {
			var err error
			fields, rows, err = project(fields, rows, query.Fields)
			if err != nil {
				return 0, err
			}
		}
	}

	defs := make([]schema.FieldDef, len(fields))
	for i, fld := range fields {
		defs[i] = schema.FieldDef{
			Name:      fld.Name,
			Type:      schema.DataType(fld.Type),
			Length:    fld.Length,
			Precision: fld.Precision,
			Scale:     fld.Scale,
			Timezone:  fld.Timezone,
			Key:       fld.Key,
			Nullable:  true,
		}
	}
	conv := schema.NewConverter()

	if _, err := io.WriteString(w, "["); err != nil {
		return 0, err
	}
	if pretty && len(rows) > 0 {
		if _, err := io.WriteString(w, "\n"); err != nil {
			return 0, err
		}
	}
	for i, values := range rows {
		obj, err := marshalObject(fields, defs, values, conv)
		if err != nil {
			return 0, fmt.Errorf("row %d: %w", i+1, err)
		}
		if pretty {
			obj = append([]byte("  "), obj...)
		}
		if i > 0 {
			if _, err := io.WriteString(w, ","); err != nil {
				return 0, err
			}
		}
		if pretty {
			if _, err := io.WriteString(w, "\n"); err != nil {
				return 0, err
			}
		}
		if _, err := w.Write(obj); err != nil {
			return 0, err
		}
	}
	if pretty && len(rows) > 0 {
		if _, err := io.WriteString(w, "\n"); err != nil {
			return 0, err
		}
	}
	if _, err := io.WriteString(w, "]"); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// project keeps only the requested columns, in requested order
// (case-insensitive lookup, like the CSV converter).
func project(fields []packet.Field, rows [][]string, want []string) ([]packet.Field, [][]string, error) {
	nameIdx := make(map[string]int, len(fields))
	for i, f := range fields {
		nameIdx[strings.ToLower(f.Name)] = i
	}
	out := make([]packet.Field, 0, len(want))
	idx := make([]int, 0, len(want))
	for _, name := range want {
		i, ok := nameIdx[strings.ToLower(name)]
		if !ok {
			return nil, nil, fmt.Errorf("--fields: column %q not found in schema", name)
		}
		out = append(out, fields[i])
		idx = append(idx, i)
	}
	proj := make([][]string, len(rows))
	for r, row := range rows {
		sel := make([]string, len(idx))
		for j, i := range idx {
			if i < len(row) {
				sel[j] = row[i]
			}
		}
		proj[r] = sel
	}
	return out, proj, nil
}

// marshalObject renders one row as a JSON object with schema-ordered keys.
func marshalObject(fields []packet.Field, defs []schema.FieldDef, values []string, conv *schema.Converter) ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, fld := range fields {
		var raw string
		if i < len(values) {
			raw = values[i]
		}
		tv, err := conv.ParseValue(raw, defs[i])
		if err != nil || tv.IsNull {
			writeNullField(&b, fld.Name, i == 0)
			continue
		}
		val, err := jsonValue(tv, defs[i].Type)
		if err != nil {
			return nil, err
		}
		writeField(&b, fld.Name, val, i == 0)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// jsonValue converts one parsed value to its JSON encoding.
func jsonValue(tv *schema.TypedValue, fieldType schema.DataType) ([]byte, error) {
	switch {
	case tv.IntValue != nil:
		v := *tv.IntValue
		if v > maxSafeJSInt || v < -maxSafeJSInt {
			return json.Marshal(strconv.FormatInt(v, 10))
		}
		return json.Marshal(v)
	case tv.FloatValue != nil:
		v := *tv.FloatValue
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return []byte("null"), nil
		}
		return json.Marshal(v)
	case tv.BoolValue != nil:
		return json.Marshal(*tv.BoolValue)
	case tv.TimeValue != nil:
		if fieldType == schema.TypeDate {
			return json.Marshal(tv.TimeValue.Format("2006-01-02"))
		}
		return json.Marshal(tv.TimeValue.UTC().Format("2006-01-02T15:04:05Z07:00"))
	case tv.StringValue != nil:
		if *tv.StringValue == packet.SpecNullMarker {
			return []byte("null"), nil
		}
		return json.Marshal(*tv.StringValue)
	default:
		if tv.RawValue == "" {
			return []byte("null"), nil
		}
		return json.Marshal(tv.RawValue)
	}
}

func writeField(b *strings.Builder, name string, val []byte, first bool) {
	if !first {
		b.WriteByte(',')
	}
	key, _ := json.Marshal(name)
	b.Write(key)
	b.WriteByte(':')
	b.Write(val)
}

func writeNullField(b *strings.Builder, name string, first bool) {
	writeField(b, name, []byte("null"), first)
}

// WritePacket is Write for a stored packet: decompresses, verifies local
// xxh3 integrity when stamped, and expands compact/columnar layouts first.
// Encrypted packets are refused (hashes would cover ciphertext).
func WritePacket(ctx context.Context, w io.Writer, pkt *packet.DataPacket, query *packet.Query, pretty bool) (int, error) {
	if pkt.Data.Encryption != "" {
		return 0, fmt.Errorf("refusing to render encrypted content as JSON: decrypt first")
	}
	if pkt.Data.Compression != "" {
		if err := processors.DecompressPacket(ctx, pkt); err != nil {
			return 0, fmt.Errorf("decompression failed: %w", err)
		}
	}
	if packet.HasIntegrity(pkt) {
		if err := packet.VerifyIntegrity(pkt); err != nil {
			return 0, fmt.Errorf("integrity: %w", err)
		}
	}
	if pkt.Data.Layout == packet.LayoutColumns {
		if err := packet.ExpandColumnarRows(pkt); err != nil {
			return 0, fmt.Errorf("columnar expansion failed: %w", err)
		}
	}
	if pkt.Data.Compact {
		if err := packet.ExpandCompactRows(pkt); err != nil {
			return 0, fmt.Errorf("compact expansion failed: %w", err)
		}
	}
	return Write(ctx, w, pkt, query, pretty)
}
