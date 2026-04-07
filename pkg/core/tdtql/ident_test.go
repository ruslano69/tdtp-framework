package tdtql

import "testing"

func TestStripBrackets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Clean name", "ZTR$Employee", "ZTR$Employee"},
		{"Simple brackets", "[ZTR$Employee]", "ZTR$Employee"},
		{"Schema and table", "[dbo].[Orders]", "dbo.Orders"},
		{"Mixed", "dbo.[Orders]", "dbo.Orders"},
		{"No brackets schema", "dbo.Orders", "dbo.Orders"},
		// MSSQL double-quoting prevention: bracket-quoted input must not produce [[name]]
		// when re-quoted by the SQL builder. StripBrackets must unwrap first.
		{"Dollar sign table", "[ZTR$Employee]", "ZTR$Employee"},
		{"Space in name", "[Staff Change Log]", "Staff Change Log"},
		{"Schema dollar sign", "[dbo].[ZTR$Employee]", "dbo.ZTR$Employee"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripBrackets(tt.input)
			if got != tt.expected {
				t.Errorf("StripBrackets(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSplitFieldList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"Simple", "No_,FullName,Last Name", []string{"No_", "FullName", "Last Name"}},
		{"With brackets", "No_,FullName,[Last Name]", []string{"No_", "FullName", "Last Name"}},
		{"With spaces", " No_ , FullName , [Last Name] ", []string{"No_", "FullName", "Last Name"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitFieldList(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("got %d fields, want %d", len(got), len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("field[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}
