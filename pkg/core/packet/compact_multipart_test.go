package packet

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestApplyCompact_MultipartIsolation(t *testing.T) {
	for _, name := range []string{"dept", "_dept"} {
		for _, tail := range []bool{false, true} {
			for _, fixed := range []bool{false, true} {
				for _, emptyAt := range []int{1, 11} {
					t.Run(fmt.Sprintf("%s/tail=%v/fixed=%v/empty=%d", name, tail, fixed, emptyAt), func(t *testing.T) {
						sch := Schema{Fields: []Field{{Name: name, Type: "TEXT", Fixed: fixed}, {Name: "payload", Type: "TEXT"}}}
						originalFields := append([]Field(nil), sch.Fields...)
						rows := make([][]string, 12)
						for i := range rows {
							rows[i] = []string{"Sales", strings.Repeat("x", 1000)}
						}
						rows[emptyAt][0] = ""
						g := NewGenerator()
						g.SetMaxMessageSize(8192)
						parts, err := g.GenerateReference("t", sch, rows)
						if err != nil {
							t.Fatal(err)
						}
						if len(parts) < 2 {
							t.Fatal("fixture must produce multiple parts")
						}
						var got [][]string
						for i, p := range parts {
							if _, err := ApplyCompactReport(p, []string{name}, tail); err != nil {
								t.Fatal(err)
							}
							if i+1 < len(parts) && !reflect.DeepEqual(parts[i+1].Schema.Fields, originalFields) {
								t.Fatal("compacting one part changed the next part's schema")
							}
							x, err := g.ToXML(p, false)
							if err != nil {
								t.Fatal(err)
							}
							back, err := NewParser().ParseBytes(x)
							if err != nil {
								t.Fatal(err)
							}
							if err := ExpandCompactRows(back); err != nil {
								t.Fatal(err)
							}
							got = append(got, back.GetRows()...)
						}
						if !reflect.DeepEqual(got, rows) {
							t.Fatal("multipart compact changed source cells")
						}
						if sch.Fields[0].Name != name || sch.Fields[0].Fixed != fixed {
							t.Fatal("input schema mutated")
						}
					})
				}
			}
		}
	}
}

func TestApplyCompact_PreexistingFixedEmpty(t *testing.T) {
	p := NewDataPacket(TypeReference, "t")
	p.Schema = Schema{Fields: []Field{{Name: "dept", Type: "TEXT", Fixed: true}}}
	p.SetRows([][]string{{"Sales"}, {""}})
	if _, err := ApplyCompactReport(p, nil, false); err != nil {
		t.Fatal(err)
	}
	if p.Schema.Fields[0].Fixed {
		t.Fatal("pre-existing fixed field with empty values must be cleared")
	}
	if err := ExpandCompactRows(p); err != nil {
		t.Fatal(err)
	}
	if got := p.GetRows(); !reflect.DeepEqual(got, [][]string{{"Sales"}, {""}}) {
		t.Fatalf("changed rows: %q", got)
	}
}
