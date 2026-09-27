package main

// version_initconfig_test.go — version (command + flag form) and
// init-config (byte-identical sample files, via the shared builders).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
	"github.com/ruslano69/tdtp-framework/pkg/core/version"
)

func TestVersion_Command(t *testing.T) {
	code, stdout, _ := runApp(t, "version")
	if code != ExitOK {
		t.Fatalf("exit = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(stdout, version.Version) {
		t.Errorf("output should carry %q, got %q", version.Version, stdout)
	}
}

func TestVersion_FlagForm(t *testing.T) {
	// v1 spelling: a bare --version prints the banner, exit 0.
	code, stdout, _ := runApp(t, "--version")
	if code != ExitOK {
		t.Fatalf("exit = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(stdout, version.Version) {
		t.Errorf("output should carry %q, got %q", version.Version, stdout)
	}
}

func TestInitConfig_ByteIdentical(t *testing.T) {
	for _, db := range []string{"postgres", "mssql", "mysql", "sqlite"} {
		dir := t.TempDir()
		got := filepath.Join(dir, "config.yaml")
		code, _, _ := runApp(t, "init-config", db, "--output", got)
		if code != ExitOK {
			t.Fatalf("%s: exit = %d", db, code)
		}
		// Same builders v1's create-config-* uses: identical bytes.
		want := filepath.Join(dir, "want.yaml")
		if err := cliconfig.SaveConfig(want, cliconfig.CreateSampleConfig(db)); err != nil {
			t.Fatal(err)
		}
		gotData, _ := os.ReadFile(got)
		wantData, _ := os.ReadFile(want)
		if string(gotData) != string(wantData) {
			t.Errorf("%s: content differs from v1's create-config output", db)
		}
	}
}

func TestInitConfig_BadType(t *testing.T) {
	code, _, _ := runApp(t, "init-config", "oracle")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}
