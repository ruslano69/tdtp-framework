package sanitize

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func TestSanitizeFieldName_NoOp(t *testing.T) {
	// Without any option, name must pass through unchanged
	cases := []string{"id", "user_name", "order123", "_private", "CamelCase"}
	for _, name := range cases {
		r := SanitizeFieldName(name, Options{})
		if r.SafeName != name {
			t.Errorf("no-op: %q → %q, want unchanged", name, r.SafeName)
		}
	}
}

func TestSanitizeFieldName_Clear(t *testing.T) {
	opts := Options{Clear: true}

	cases := []struct {
		input string
		want  string
	}{
		{"Total %", "Total_pct"},
		{"Cost ($)", "Cost_usd"},
		{"Order #", "Order_xh"},
		{"E-mail", "E_mail"},
		{"First Name", "First_Name"},
		{"Price/Unit", "Price_Unit"},
		{"Field.Name", "Field_Name"},
		{"a&b", "a_and_b"},
		{"x@y", "x_at_y"},
		{"val*", "val_star"},
		{"is?", "is_is"},
		{"~flag", "not_flag"},
		{"a+b", "a_plus_b"},
		{"a=b", "a_eq_b"},
		{"a!b", "a_bang_b"},
		{"a^b", "a_hat_b"},
		{"a<b", "a_lt_b"},
		{"a>b", "a_gt_b"},
		{"a№b", "a_no_b"},
		// Verify multi-char tokens don't have trailing _ when at end of name
		{"profit&loss", "profit_and_loss"},
		{"cost%", "cost_pct"},
		{"#index", "xh_index"},
		// leading digit
		{"1stField", "_1stField"},
		// empty after stripping
		{"---", "_field"},
		// consecutive specials collapse
		{"a..b", "a_b"},
		{"a  b", "a_b"},
	}

	for _, tc := range cases {
		r := SanitizeFieldName(tc.input, opts)
		if r.SafeName != tc.want {
			t.Errorf("clear %q: got %q, want %q", tc.input, r.SafeName, tc.want)
		}
		if r.OriginalName != tc.input {
			t.Errorf("clear %q: OriginalName=%q, want %q", tc.input, r.OriginalName, tc.input)
		}
	}
}

func TestSanitizeFieldName_Translit(t *testing.T) {
	opts := Options{Translit: true}

	cases := []struct {
		input string
		want  string
	}{
		{"Имя", "Imia"},
		{"Фамилия", "Familiia"},
		{"id", "id"}, // plain ASCII stays
	}

	for _, tc := range cases {
		r := SanitizeFieldName(tc.input, opts)
		if r.SafeName != tc.want {
			t.Errorf("translit %q: got %q, want %q", tc.input, r.SafeName, tc.want)
		}
	}
}

func TestSanitizeFieldName_TranslitAndClear(t *testing.T) {
	opts := Options{Translit: true, Clear: true}

	cases := []struct {
		input string
		want  string
	}{
		{"Дата рождения", "Data_rozhdeniia"},
		{"Österreich", "Osterreich"},
		{"Ñoño", "Nono"},
		{"Cena (zł)", "Cena_zl"},
		{"Precio %", "Precio_pct"},
		// № → unidecode "No." → clear: "." → "_" → "Zakaz_No_12"
		{"Заказ №12", "Zakaz_No_12"},
		{"имя.фамилия", "imia_familiia"},
		{"Béla-Márton", "Bela_Marton"},
	}

	for _, tc := range cases {
		r := SanitizeFieldName(tc.input, opts)
		if r.SafeName != tc.want {
			t.Errorf("translit+clear %q: got %q, want %q", tc.input, r.SafeName, tc.want)
		}
	}
}

func TestApplyToSchema(t *testing.T) {
	schema := &packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "Total %", Type: "FLOAT"},
			{Name: "First Name", Type: "TEXT"},
			{Name: "email", Type: "TEXT"},
			{Name: "Cost ($)", Type: "FLOAT"},
		},
	}

	changed := ApplyToSchema(schema, Options{Clear: true})

	if len(changed) != 3 {
		t.Fatalf("expected 3 changed, got %d", len(changed))
	}

	// Check safe names
	want := []string{"id", "Total_pct", "First_Name", "email", "Cost_usd"}
	for i, f := range schema.Fields {
		if f.Name != want[i] {
			t.Errorf("field[%d]: got %q, want %q", i, f.Name, want[i])
		}
	}

	// Verify OriginalName is set only for changed fields
	if schema.Fields[0].OriginalName != "" {
		t.Error("id should not have OriginalName set")
	}
	if schema.Fields[1].OriginalName != "Total %" {
		t.Errorf("Total %%: OriginalName=%q", schema.Fields[1].OriginalName)
	}
	if schema.Fields[3].OriginalName != "" {
		t.Error("email should not have OriginalName set")
	}
}

func TestApplyToSchema_NoChange(t *testing.T) {
	schema := &packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
		},
	}

	changed := ApplyToSchema(schema, Options{Clear: true})
	if len(changed) != 0 {
		t.Errorf("expected 0 changed, got %d", len(changed))
	}
}

func TestApplyToSchema_Disabled(t *testing.T) {
	schema := &packet.Schema{
		Fields: []packet.Field{
			{Name: "Total %", Type: "FLOAT"},
		},
	}

	changed := ApplyToSchema(schema, Options{})
	if len(changed) != 0 {
		t.Error("disabled options must not change anything")
	}
	if schema.Fields[0].Name != "Total %" {
		t.Error("field name must not be changed when options disabled")
	}
}
