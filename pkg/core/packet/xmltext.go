package packet

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/xmlchar"
)

// TDTP text is UTF-8 with nothing XML 1.0 forbids — in every section, not
// only in rows. The checks here are what makes that a rule rather than a
// hope.
//
// Before them the writer handed Header, Query and Schema to xml.Marshal,
// which silently replaces a forbidden character with U+FFFD: a table named
// "ta\x01ble" was exported as "ta\uFFFDble" and imported under that name,
// a column read from a single-byte source as "caf\xe9" became "caf\uFFFD".
// Rows went the other way — written raw, so the file was not XML at all
// (encoding/xml refuses it) and was readable only while our fast path
// handled it. --fast even wrote every NULL as a raw 0x00 byte.

// textError names where a forbidden spot was found.
type textError struct {
	where string
	bad   xmlchar.Bad
}

func (e *textError) Error() string {
	return fmt.Sprintf("%s: %s — TDTP text must be UTF-8 that XML 1.0 can carry", e.where, e.bad)
}

// sectionsClean reports whether every exported string reachable from v is
// text XML can carry. It builds nothing and allocates nothing — the common
// case, a clean packet, costs only the walk. sectionError then redoes the
// walk with paths to say where the bad text is.
func sectionsClean(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return xmlchar.Clean(v.String())
	case reflect.Pointer, reflect.Interface:
		return v.IsNil() || sectionsClean(v.Elem())
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).IsExported() && !sectionsClean(v.Field(i)) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return true // []byte is not text here
		}
		for i := 0; i < v.Len(); i++ {
			if !sectionsClean(v.Index(i)) {
				return false
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if !sectionsClean(iter.Key()) || !sectionsClean(iter.Value()) {
				return false
			}
		}
	}
	return true
}

// sectionError finds the bad string sectionsClean rejected and names it,
// e.g. "Schema.Fields[1].Name".
func sectionError(path string, v reflect.Value) error {
	switch v.Kind() {
	case reflect.String:
		if b, bad := xmlchar.Find(v.String()); bad {
			return &textError{where: path, bad: b}
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return sectionError(path, v.Elem())
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if !t.Field(i).IsExported() {
				continue
			}
			if err := sectionError(path+"."+t.Field(i).Name, v.Field(i)); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return nil
		}
		for i := 0; i < v.Len(); i++ {
			if err := sectionError(fmt.Sprintf("%s[%d]", path, i), v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			key := fmt.Sprint(iter.Key().Interface())
			if err := sectionError(path+".key("+key+")", iter.Key()); err != nil {
				return err
			}
			if err := sectionError(path+"["+key+"]", iter.Value()); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkPacketText refuses a packet whose sections or root/Data attributes
// hold text XML cannot carry. Rows are checked by the writer as it writes
// them. Sections are reached through pointers, so nothing is boxed.
func checkPacketText(p *DataPacket) error {
	sections := [...]struct {
		name string
		v    reflect.Value
	}{
		{"Header", reflect.ValueOf(&p.Header)},
		{"Query", reflect.ValueOf(p.Query)},
		{"QueryContext", reflect.ValueOf(p.QueryContext)},
		{"PipelineContext", reflect.ValueOf(p.PipelineContext)},
		{"Schema", reflect.ValueOf(&p.Schema)},
		{"AlarmDetails", reflect.ValueOf(p.AlarmDetails)},
	}
	for _, sec := range sections {
		if !sectionsClean(sec.v) {
			return sectionError(sec.name, sec.v)
		}
	}
	attrs := [...]struct{ name, v string }{
		{"protocol", p.Protocol}, {"version", p.Version}, {"xxh3", p.XXH3},
		{"Data.carry", p.Data.Carry},
	}
	for _, a := range attrs {
		if b, bad := xmlchar.Find(a.v); bad {
			return &textError{where: a.name, bad: b}
		}
	}
	return nil
}

// rowFieldAt names the field an offset in an escaped, pipe-joined row falls
// in, for the error message.
func rowFieldAt(row string, offset int, sch Schema) string {
	idx := 0
	for i := 0; i < offset && i < len(row); i++ {
		switch row[i] {
		case '\\':
			i++ // escaped character: \| is not a separator
		case '|':
			idx++
		}
	}
	if idx < len(sch.Fields) {
		return fmt.Sprintf("field %q", sch.Fields[idx].Name)
	}
	return fmt.Sprintf("field #%d", idx+1)
}

func rowWhere(p *DataPacket, row int, field string) string {
	var b strings.Builder
	if p.Header.PartNumber > 0 {
		fmt.Fprintf(&b, "part %d, ", p.Header.PartNumber)
	}
	fmt.Fprintf(&b, "row %d, %s", row+1, field)
	return b.String()
}
