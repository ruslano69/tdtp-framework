package tdtql

import (
	"strings"
	"testing"
)

// Anything after the last recognized clause used to be dropped in silence.
// The worst case is a typo: "AMD" for "AND" filtered on the first condition
// alone and returned MORE rows than the caller asked for.
func TestTranslateWhere_RejectsTrailingTokens(t *testing.T) {
	tr := NewTranslator()
	for _, where := range []string{
		"dept = 'hr' AMD id > 5",
		"dept = 'hr' garbage",
		"dept = 'hr')",
		"dept = 'hr' ;;",
		"dept = 'hr' ORDER BY id", // parsed, then dropped: only filters return
		"dept = 'hr' LIMIT 5",
	} {
		if _, err := tr.TranslateWhere(where); err == nil {
			t.Errorf("%q: accepted, want an error", where)
		}
	}
	for _, where := range []string{
		"dept = 'hr'",
		"dept = 'hr' AND id > 5",
		"(dept = 'hr' OR dept = 'it') AND id > 1",
		"dept = 'hr';", // one trailing ';' is ordinary SQL
	} {
		if _, err := tr.TranslateWhere(where); err != nil {
			t.Errorf("%q: %v", where, err)
		}
	}
}

func TestTranslateOrderBy(t *testing.T) {
	tr := NewTranslator()
	ob, err := tr.TranslateOrderBy("name ASC, age DESC")
	if err != nil {
		t.Fatal(err)
	}
	if len(ob.Fields) != 2 || ob.Fields[1].Name != "age" || ob.Fields[1].Direction != "DESC" {
		t.Errorf("order = %+v", ob)
	}
	for _, bad := range []string{"dept SIDEWAYS", "dept DESC extra", "id LIMIT 5", "id OFFSET 2"} {
		if _, err := tr.TranslateOrderBy(bad); err == nil {
			t.Errorf("%q: accepted, want an error", bad)
		}
	}
}

func TestTranslate_FullStatementEndsAtEOF(t *testing.T) {
	tr := NewTranslator()
	if _, err := tr.Translate("SELECT * FROM t WHERE a = 1 ORDER BY a DESC LIMIT 10 OFFSET 5;"); err != nil {
		t.Errorf("full statement: %v", err)
	}
	_, err := tr.Translate("SELECT * FROM t WHERE a = 1 LIMIT 10 surplus")
	if err == nil || !strings.Contains(err.Error(), `"surplus"`) {
		t.Errorf("want the stray token named in the error, got %v", err)
	}
}
