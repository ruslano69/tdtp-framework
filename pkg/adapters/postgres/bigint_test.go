package postgres

import (
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// An integer DECIMAL hinted "bigint" (what Oracle reports for NUMBER(19,0))
// is created as BIGINT; without the hint DECIMAL keeps its numeric DDL.
func TestBigintDecimalDDL(t *testing.T) {
	f := packet.Field{Name: "id", Type: "DECIMAL", Precision: 19, Subtype: "bigint"}
	if got := TDTPToPostgreSQL(f); !strings.EqualFold(got, "BIGINT") {
		t.Errorf("DECIMAL(19,0) bigint → %q, want BIGINT", got)
	}
	f.Subtype = ""
	if got := TDTPToPostgreSQL(f); strings.EqualFold(got, "BIGINT") {
		t.Errorf("DECIMAL(19,0) without the hint → %q, must stay numeric", got)
	}
}
