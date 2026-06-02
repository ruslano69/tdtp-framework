package main

import (
	"strings"
	"testing"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]string
	}{
		{
			name: "space form",
			args: []string{"--db", "ca.db", "--licensee", "Contoso"},
			want: map[string]string{"db": "ca.db", "licensee": "Contoso"},
		},
		{
			name: "equals form",
			args: []string{"--db=ca.db", "--seat-limit=3"},
			want: map[string]string{"db": "ca.db", "seat-limit": "3"},
		},
		{
			name: "boolean flag at end",
			args: []string{"--db", "ca.db", "--force"},
			want: map[string]string{"db": "ca.db", "force": "true"},
		},
		{
			name: "mixed",
			args: []string{"--db=ca.db", "--licensee", "X", "--verbose"},
			want: map[string]string{"db": "ca.db", "licensee": "X", "verbose": "true"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFlags(tt.args)
			for k, want := range tt.want {
				if got[k] != want {
					t.Errorf("flag %q = %q, want %q", k, got[k], want)
				}
			}
		})
	}
}

func TestGenerateLicenseKey(t *testing.T) {
	key, err := generateLicenseKey()
	if err != nil {
		t.Fatalf("generateLicenseKey: %v", err)
	}
	if !strings.HasPrefix(key, "TDTP-") {
		t.Errorf("license key %q missing TDTP- prefix", key)
	}
	// Format: TDTP-XXXXX-XXXXX-XXXXX-XXXXX → 5 groups separated by '-'.
	if parts := strings.Split(key, "-"); len(parts) != 5 {
		t.Errorf("license key %q has %d groups, want 5", key, len(parts))
	}
	// Two calls must differ (random).
	key2, _ := generateLicenseKey()
	if key == key2 {
		t.Error("generateLicenseKey returned identical keys")
	}
}

func TestRequireFlag(t *testing.T) {
	f := map[string]string{"db": "ca.db", "empty": ""}

	if v, err := requireFlag(f, "db"); err != nil || v != "ca.db" {
		t.Errorf("requireFlag(db) = %q, %v; want ca.db, nil", v, err)
	}
	if _, err := requireFlag(f, "missing"); err == nil {
		t.Error("requireFlag(missing) should error")
	}
	if _, err := requireFlag(f, "empty"); err == nil {
		t.Error("requireFlag(empty) should error on empty value")
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV("etl, enc ,s3,")
	want := []string{"etl", "enc", "s3"}
	if len(got) != len(want) {
		t.Fatalf("splitCSV len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitCSV[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
