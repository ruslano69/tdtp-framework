package commands

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
)

func TestAddMaskRules(t *testing.T) {
	pm := NewRowProcessors()
	if err := pm.AddMaskRules([]cliconfig.MaskRule{
		{Field: "email", Strategy: "partial"},
		{Field: "phone", Strategy: "nonsense-strategy"},
		{Field: "  "},
	}); err != nil {
		t.Fatalf("AddMaskRules: %v", err)
	}
	if !pm.HasProcessors() {
		t.Fatal("chain must hold the masker")
	}
}

func TestAddMaskRules_Empty(t *testing.T) {
	pm := NewRowProcessors()
	if err := pm.AddMaskRules(nil); err != nil {
		t.Fatal(err)
	}
	if pm.HasProcessors() {
		t.Error("empty rules must not build anything")
	}
}

func TestAddValidateRules(t *testing.T) {
	pm := NewRowProcessors()
	if err := pm.AddValidateRules([]cliconfig.ValidateRule{
		{Field: "age", Type: "range", Min: "0", Max: "150"},
		{Field: "email", Type: "email"},
		{Field: "code", Type: "regex", Pattern: "^[A-Z]+$"},
	}); err != nil {
		t.Fatalf("AddValidateRules: %v", err)
	}
	if !pm.HasProcessors() {
		t.Fatal("chain must hold the validator")
	}
}

func TestAddValidateRules_BadType(t *testing.T) {
	pm := NewRowProcessors()
	// "format" is not a rule type (email/phone/url/date are); it must
	// fail here, not silently validate nothing downstream.
	err := pm.AddValidateRules([]cliconfig.ValidateRule{
		{Field: "x", Type: "format"},
	})
	if err == nil {
		t.Fatal("unknown validation type must fail")
	}
}

func TestAddNormalizeRules(t *testing.T) {
	pm := NewRowProcessors()
	if err := pm.AddNormalizeRules([]cliconfig.NormalizeRule{
		{Field: "city", Strategy: "uppercase"},
	}); err != nil {
		t.Fatalf("AddNormalizeRules: %v", err)
	}
	if !pm.HasProcessors() {
		t.Fatal("chain must hold the normalizer")
	}
}
