package workflow

import "testing"

// TestChildArgsPrependsQuiet pins the ordering. Go's flag package stops parsing
// at the first non-flag argument, so a --quiet appended after a command that
// takes a positional would be parsed as the positional's neighbour and ignored.
func TestChildArgsPrependsQuiet(t *testing.T) {
	in := []string{"--sync-incremental", "orders", "--to-broker"}

	if got := childArgs(in, false); len(got) != 3 || got[0] != "--sync-incremental" {
		t.Errorf("quiet=false must pass the args through unchanged, got %v", got)
	}

	got := childArgs(in, true)
	if len(got) != 4 || got[0] != "--quiet" {
		t.Fatalf("expected --quiet first, got %v", got)
	}
	for i, want := range in {
		if got[i+1] != want {
			t.Errorf("arg %d: got %q, want %q", i+1, got[i+1], want)
		}
	}
}
