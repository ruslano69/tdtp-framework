package mssql

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// Live-catalog test for the schema-constraints prototype. Skips without a
// server, like the rest of this package; point MSSQL_TEST_DSN_PROD at one.
// The table is created and dropped here — the catalog, not a fixture, is
// what is under test.
func TestIntegration_ReadTableConstraints(t *testing.T) {
	ctx := context.Background()
	a, err := adapters.New(ctx, adapters.Config{Type: "mssql", DSN: testConnStringProdSim})
	if err != nil {
		t.Skipf("MS SQL Server (prod sim) not available: %v", err)
	}
	defer func() { _ = a.Close(ctx) }()
	ms := a.(*Adapter)

	table := fmt.Sprintf("tdtp_cons_%d", time.Now().UnixNano())
	exec := func(q string) {
		t.Helper()
		if _, err := ms.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	exec(`CREATE TABLE dbo.` + table + ` (
		Branch smallint NOT NULL,
		TabNo  int      NOT NULL,
		Code   varchar(10) NOT NULL CONSTRAINT DF_` + table + `_Code DEFAULT ('A1'),
		Age    tinyint NULL,
		Rate   decimal(4,2) NULL,
		Status varchar(10) NULL,
		Inn    char(10) NULL,
		Tag    varchar(10) NULL,
		Made   datetime NOT NULL CONSTRAINT DF_` + table + `_Made DEFAULT (getdate()),
		CONSTRAINT PK_` + table + ` PRIMARY KEY (TabNo, Branch),
		CONSTRAINT UQ_` + table + `_Inn UNIQUE (Branch, Inn),
		CONSTRAINT CK_` + table + `_Age CHECK (Age BETWEEN 18 AND 70),
		CONSTRAINT CK_` + table + `_Age2 CHECK (Age >= 21),
		CONSTRAINT CK_` + table + `_Rate CHECK (Rate > 0 AND Rate <= 1.5),
		CONSTRAINT CK_` + table + `_Status CHECK (Status IN ('a','b')),
		CONSTRAINT CK_` + table + `_Code CHECK (Code LIKE '[A-Z][0-9]%'),
		CONSTRAINT CK_` + table + `_Inn CHECK (LEN(Inn) = 10),
		CONSTRAINT CK_` + table + `_Tag CHECK (Tag LIKE 'x%'))`)
	defer exec(`DROP TABLE dbo.` + table)
	exec(`ALTER TABLE dbo.` + table + ` NOCHECK CONSTRAINT CK_` + table + `_Tag`)
	exec(`CREATE UNIQUE INDEX UX_` + table + `_Status ON dbo.` + table + `(Status) WHERE Status IS NOT NULL`)

	tc, err := ms.ReadTableConstraints(ctx, "dbo."+table)
	if err != nil {
		t.Fatal(err)
	}
	col := func(name string) *ColumnConstraints {
		t.Helper()
		c := tc.column(name)
		if c == nil {
			t.Fatalf("column %s missing", name)
		}
		return c
	}
	val := func(s *string) string {
		if s == nil {
			return "<nil>"
		}
		return *s
	}

	// Key order is the PK's, not the columns'.
	if tc.PrimaryKey == nil || strings.Join(tc.PrimaryKey.Columns, ",") != "TabNo,Branch" {
		t.Errorf("primary key = %+v, want (TabNo, Branch)", tc.PrimaryKey)
	}
	if len(tc.Uniques) != 1 || strings.Join(tc.Uniques[0].Columns, ",") != "Branch,Inn" {
		t.Errorf("uniques = %+v, want one (Branch, Inn)", tc.Uniques)
	}
	if col("Code").Nullable || !col("Age").Nullable {
		t.Error("nullability not read")
	}
	if val(col("Code").DefaultLiteral) != "A1" || col("Made").DefaultExpr != "(getdate())" {
		t.Errorf("defaults: Code=%s Made=%q", val(col("Code").DefaultLiteral), col("Made").DefaultExpr)
	}
	if age := col("Age"); val(age.MinInclusive) != "18" || val(age.MaxInclusive) != "70" {
		t.Errorf("Age range = [%s, %s]", val(age.MinInclusive), val(age.MaxInclusive))
	}
	if r := col("Rate"); val(r.MinExclusive) != "0" || val(r.MaxInclusive) != "1.5" {
		t.Errorf("Rate range = (%s, %s]", val(r.MinExclusive), val(r.MaxInclusive))
	}
	if s := col("Status"); strings.Join(s.Enum, ",") != "b,a" { // stored order (IN is stored reversed)
		t.Errorf("Status enum = %v", s.Enum)
	}
	// A letter pattern under a _CI_ collation is flagged; the source
	// accepted 'a1' too, the XSD pattern will not.
	if c := col("Code"); c.Pattern != `[A-Z][0-9].*` || !c.PatternCaseInsensitive {
		t.Errorf("Code pattern = %q ci=%v", c.Pattern, c.PatternCaseInsensitive)
	}

	raw := map[string]string{}
	for _, rc := range tc.RawChecks {
		raw[strings.TrimPrefix(rc.Name, "CK_"+table+"_")] = rc.Reason
	}
	// Age >= 21 conflicts with BETWEEN 18 AND 70 on the minimum: not merged,
	// not dropped — kept whole as raw text.
	if !strings.Contains(raw["Age2"], "second minimum") {
		t.Errorf("conflicting CHECK should stay raw with a reason, raw=%v", raw)
	}
	if raw["Inn"] == "" {
		t.Errorf("LEN() CHECK should be raw, raw=%v", raw)
	}
	skipped := strings.Join(tc.Skipped, "\n")
	for _, want := range []string{"CK_" + table + "_Tag is disabled", "UX_" + table + "_Status is filtered"} {
		if !strings.Contains(skipped, want) {
			t.Errorf("skipped should mention %q:\n%s", want, skipped)
		}
	}
	if col("Tag").Pattern != "" {
		t.Error("a disabled CHECK must not become a facet")
	}
}
