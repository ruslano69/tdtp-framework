package mssql

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/jacoelho/xsd"
)

// The inputs are the definitions SQL Server 2019 actually stored for the
// probe table in docs/proposals/schema-constraints.md — not the DDL as
// written. BETWEEN and IN never reach sys.check_constraints.

func str(s string) *string { return &s }

func eqPtr(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func TestClassifyCheck_StoredForms(t *testing.T) {
	cases := []struct {
		name, def string
		want      CheckFacets // zero value + !recognized = must not be recognized
		ok        bool
	}{
		{"BETWEEN", `([Age]>=(18) AND [Age]<=(70))`,
			CheckFacets{Column: "Age", MinInclusive: str("18"), MaxInclusive: str("70")}, true},
		{"one bound", `([Salary]>=(0))`,
			CheckFacets{Column: "Salary", MinInclusive: str("0")}, true},
		{"exclusive + decimal", `([Rate]>(0) AND [Rate]<=(1.5))`,
			CheckFacets{Column: "Rate", MinExclusive: str("0"), MaxInclusive: str("1.5")}, true},
		{"negative literal", `([T]>=(-40) AND [T]<(60))`,
			CheckFacets{Column: "T", MinInclusive: str("-40"), MaxExclusive: str("60")}, true},
		// Enumerations keep the STORED order: IN is stored reversed, a
		// hand-written OR is not, and the text cannot tell them apart.
		{"IN strings", `([Status]='fired' OR [Status]='leave' OR [Status]='active')`,
			CheckFacets{Column: "Status", Enum: []string{"fired", "leave", "active"}}, true},
		{"IN numbers", `([Grade]=(8) OR [Grade]=(5) OR [Grade]=(3) OR [Grade]=(2) OR [Grade]=(1))`,
			CheckFacets{Column: "Grade", Enum: []string{"8", "5", "3", "2", "1"}}, true},
		{"quote in literal", `([Name]='O''Brien')`,
			CheckFacets{Column: "Name", Enum: []string{"O'Brien"}}, true},
		{"LIKE class", `([Zip] like '[0-9][0-9][0-9][0-9][0-9]')`,
			CheckFacets{Column: "Zip", Pattern: `[0-9][0-9][0-9][0-9][0-9]`}, true},
		{"LIKE alternatives", `([Phone] like '+7%' OR [Phone] like '8%')`,
			CheckFacets{Column: "Phone", Pattern: `(\+7.*|8.*)`}, true},
		{"LIKE email-ish", `([Email] like '%_@_%._%')`,
			CheckFacets{Column: "Email", Pattern: `.*.@..*\...*`}, true},
		{"IS NULL OR range", `([X] IS NULL OR [X]>=(1))`,
			CheckFacets{Column: "X", MinInclusive: str("1")}, true},

		{"function", `(len([Inn])=(10))`, CheckFacets{}, false},
		{"two columns", `([FiredAt] IS NULL OR [FiredAt]>=[HiredAt])`, CheckFacets{}, false},
		{"not equal", `([Code]<>'X')`, CheckFacets{}, false},
		{"mixed OR", `([A]=(1) OR [A] like 'x%')`, CheckFacets{}, false},
		{"NOT", `(NOT [A]=(1))`, CheckFacets{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyCheck(tc.def)
			if got.Recognized != tc.ok {
				t.Fatalf("recognized=%v, want %v (reason: %s)", got.Recognized, tc.ok, got.Reason)
			}
			if !tc.ok {
				if got.Reason == "" {
					t.Error("an unrecognized CHECK must say why")
				}
				return
			}
			f := got.Facets
			if f.Column != tc.want.Column || f.Pattern != tc.want.Pattern ||
				!eqPtr(f.MinInclusive, tc.want.MinInclusive) || !eqPtr(f.MinExclusive, tc.want.MinExclusive) ||
				!eqPtr(f.MaxInclusive, tc.want.MaxInclusive) || !eqPtr(f.MaxExclusive, tc.want.MaxExclusive) ||
				strings.Join(f.Enum, "|") != strings.Join(tc.want.Enum, "|") {
				t.Errorf("facets = %s, want %s", dumpFacets(f), dumpFacets(tc.want))
			}
		})
	}
}

func dumpFacets(f CheckFacets) string {
	p := func(s *string) string {
		if s == nil {
			return "-"
		}
		return *s
	}
	return fmt.Sprintf("{%s min=%s/%s max=%s/%s enum=%v pattern=%q}", f.Column,
		p(f.MinInclusive), p(f.MinExclusive), p(f.MaxInclusive), p(f.MaxExclusive), f.Enum, f.Pattern)
}

func TestClassifyDefault(t *testing.T) {
	for def, want := range map[string]string{
		`((0))`: "0", `((-1))`: "-1", `((1.5))`: "1.5", `('active')`: "active", `(N'текст')`: "текст",
	} {
		if got, ok := ClassifyDefault(def); !ok || got != want {
			t.Errorf("%s → %q ok=%v, want %q", def, got, ok, want)
		}
	}
	for _, def := range []string{`(getdate())`, `(newid())`, `(dateadd(day,(1),getdate()))`} {
		if _, ok := ClassifyDefault(def); ok {
			t.Errorf("%s is an expression, not a literal", def)
		}
	}
}

// One converted pattern must mean the same to the two engines that will
// read it: an XSD validator (the schema contract other SDKs use) and Go's
// regexp (our validator). Checked against both, on values LIKE accepts
// and rejects.
func TestLikeToXSDPattern_SameInXSDAndGo(t *testing.T) {
	cases := []struct {
		like, escape string
		match, no    []string
	}{
		{`[0-9][0-9][0-9]`, "", []string{"123"}, []string{"12a", "1234", "12"}},
		{`+7%`, "", []string{"+7", "+79001234567"}, []string{"79001234567", "x+7"}},
		{`%_@_%._%`, "", []string{"a@b.c", "ivan@mail.ru"}, []string{"@b.c", "a@b", "ab.c"}},
		{`A_C`, "", []string{"ABC", "A.C"}, []string{"AC", "ABBC"}},
		{`$%`, "", []string{"$", "$5"}, []string{"5$"}},
		{`[^a-c]x`, "", []string{"dx", "1x"}, []string{"ax", "cx"}},
		{`100!%`, "!", []string{"100%"}, []string{"1000", "100"}},
		{`a.b(c)`, "", []string{"a.b(c)"}, []string{"aXb(c)"}},
	}
	for _, tc := range cases {
		p, err := likeToXSDPattern(tc.like, tc.escape)
		if err != nil {
			t.Fatalf("%q: %v", tc.like, err)
		}
		goRe := regexp.MustCompile(`^(?:` + p + `)$`)
		schema := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">` +
			`<xs:element name="v"><xs:simpleType><xs:restriction base="xs:string">` +
			`<xs:pattern value="` + xmlAttr(p) + `"/></xs:restriction></xs:simpleType></xs:element></xs:schema>`
		eng, err := xsd.Compile(xsd.Bytes("p.xsd", []byte(schema)))
		if err != nil {
			t.Fatalf("%q → %q is not a valid XSD pattern: %v", tc.like, p, err)
		}
		check := func(v string, want bool) {
			xsdOK := eng.Validate(strings.NewReader("<v>"+xmlText(v)+"</v>")) == nil
			if goRe.MatchString(v) != want || xsdOK != want {
				t.Errorf("LIKE %q → %q on %q: go=%v xsd=%v, want %v", tc.like, p, v, goRe.MatchString(v), xsdOK, want)
			}
		}
		for _, v := range tc.match {
			check(v, true)
		}
		for _, v := range tc.no {
			check(v, false)
		}
	}
}

func xmlAttr(s string) string {
	return strings.NewReplacer(`&`, "&amp;", `"`, "&quot;", `<`, "&lt;").Replace(s)
}

func xmlText(s string) string {
	return strings.NewReplacer(`&`, "&amp;", `<`, "&lt;").Replace(s)
}
