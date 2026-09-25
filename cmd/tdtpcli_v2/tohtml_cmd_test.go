package main

// tohtml_cmd_test.go — to-html through the dispatcher. The produced FILE
// is proven byte-identical to v1 E2E; here the contract and flag handling.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToHTML_Basic(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "u.html")
	code, _, _ := runApp(t, "to-html", in, "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	html := string(data)
	for _, want := range []string{"<table", "John", "Balance"} {
		if !strings.Contains(html, want) {
			t.Errorf("output should contain %q", want)
		}
	}
}

func TestToHTML_RowRange(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "r.html")
	code, _, _ := runApp(t, "to-html", in, "--output", out, "--row", "1-1")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	if strings.Contains(string(data), "Bob") {
		t.Error("row range 1-1 must not render Bob (row 3)")
	}
}

func TestToHTML_Filter(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "f.html")
	code, _, _ := runApp(t, "to-html", in, "--output", out, "--where", "Balance > 1000")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	if strings.Contains(string(data), "Bob") {
		t.Error("Balance > 1000 must not render Bob (500)")
	}
}

func TestToHTML_MissingFile(t *testing.T) {
	code, _, _ := runApp(t, "to-html", filepath.Join(t.TempDir(), "nope.xml"))
	if code != ExitFail {
		t.Errorf("exit = %d, want %d", code, ExitFail)
	}
}

func TestParseRowRange(t *testing.T) {
	cases := []struct {
		in         string
		start, end int
	}{
		{"", 0, 0},
		{"100-150", 100, 150},
		{"100", 100, 0},
		{"bogus", 0, 0},
		{" 10 - 20 ", 10, 20},
	}
	for _, c := range cases {
		if s, e := parseRowRange(c.in); s != c.start || e != c.end {
			t.Errorf("parseRowRange(%q) = %d,%d; want %d,%d", c.in, s, e, c.start, c.end)
		}
	}
}

func TestCompat_ToHTMLResolves(t *testing.T) {
	got, _, ok := compatResolve([]string{"--to-html", "f.xml"})
	if !ok || len(got) != 2 || got[0] != "to-html" {
		t.Errorf("--to-html rewrote to %v", got)
	}
}
