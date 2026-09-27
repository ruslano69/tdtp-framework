package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestStepsCmd_Validation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"missing workflow", []string{"steps"}},
		{"malformed variable", []string{"steps", "workflow.yaml", "@name"}},
		{"unexpected positional", []string{"steps", "workflow.yaml", "extra.yaml"}},
		{"foreign flag", []string{"steps", "workflow.yaml", "--delimiter", ";"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := runApp(t, tc.args...)
			if code != ExitUsage || stderr == "" {
				t.Fatalf("exit=%d stderr=%q, want a visible usage error", code, stderr)
			}
		})
	}
	if got, _, ok := compatResolve([]string{"--steps", "workflow.yaml", "@date=2026-09-27"}); !ok || strings.Join(got, " ") != "steps workflow.yaml @date=2026-09-27" {
		t.Fatalf("legacy --steps resolved to %v (ok=%v)", got, ok)
	}
}

func TestStepsCmd_EndToEnd(t *testing.T) {
	// Build the actual CLI: os.Executable() must be tdtpcli_v2.exe inside the
	// workflow, not this test binary or the v1 CLI found on PATH.
	dir := t.TempDir()
	bin := filepath.Join(dir, "tdtpcli_v2")
	if os.PathSeparator == '\\' {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build v2 CLI: %v\n%s", err, output)
	}
	input, err := filepath.Abs("../../docs/samples/employees-plain.tdtp")
	if err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(dir, "result file.json")
	wf := filepath.Join(dir, "workflow.yaml")
	yaml := fmt.Sprintf("name: v2-steps\nsteps:\n"+
		"  - id: convert\n    command: to-json %q --output \"{{destination}}\"\n"+
		"  - id: verify\n    command: --test %q\n    depends_on: [convert]\n",
		filepath.ToSlash(input), filepath.ToSlash(input))
	if err := os.WriteFile(wf, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
		}
		return stdout.String(), stderr.String()
	}

	variable := "@destination=" + filepath.ToSlash(outFile)
	stdout, _ := run("steps", wf, variable)
	if !strings.Contains(stdout, "[steps] ✓  convert") || !strings.Contains(stdout, "[steps] ✓  verify") {
		t.Fatalf("dependent steps did not complete: %q", stdout)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("v2-only child command did not write JSON: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		t.Fatalf("child output is not nonempty JSON: %v, %q", err, data)
	}

	stdout, stderr := run("--quiet", "--steps", wf, variable)
	if !strings.Contains(stderr, "--steps is deprecated") || strings.Contains(stdout, "▶  convert:") {
		t.Fatalf("legacy shim or quiet propagation failed: stdout=%q stderr=%q", stdout, stderr)
	}
	stdout, stderr = run("--json", "steps", wf, variable)
	var verdict struct {
		Valid    bool   `json:"valid"`
		Workflow string `json:"workflow"`
	}
	if err := json.Unmarshal([]byte(stdout), &verdict); err != nil || !verdict.Valid || verdict.Workflow != wf {
		t.Fatalf("stdout must hold one workflow JSON verdict: %v, %q", err, stdout)
	}
	if !strings.Contains(stderr, "[steps] ✓  verify") {
		t.Fatalf("JSON mode lost workflow progress on stderr: %q", stderr)
	}

	skippedOut := filepath.Join(dir, "skipped.json")
	independentOut := filepath.Join(dir, "independent.json")
	skipWF := filepath.Join(dir, "skip.yaml")
	skipYAML := fmt.Sprintf("name: skip-chain\nsteps:\n"+
		"  - id: fails\n    command: test %q\n    on_error: skip\n"+
		"  - id: dependent\n    command: to-json %q --output %q\n    depends_on: [fails]\n"+
		"  - id: independent\n    command: to-json %q --output %q\n",
		filepath.ToSlash(filepath.Join(dir, "missing.tdtp.xml")),
		filepath.ToSlash(input), filepath.ToSlash(skippedOut),
		filepath.ToSlash(input), filepath.ToSlash(independentOut))
	if err := os.WriteFile(skipWF, []byte(skipYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _ = run("steps", skipWF)
	if !strings.Contains(stdout, "dependent — skipped") {
		t.Fatalf("skip did not propagate: %q", stdout)
	}
	if _, err := os.Stat(skippedOut); !os.IsNotExist(err) {
		t.Fatalf("skipped dependent created an output: %v", err)
	}
	if _, err := os.Stat(independentOut); err != nil {
		t.Fatalf("independent step did not complete: %v", err)
	}

	// The unchanged v1 engine and the v2 shim must produce the same workflow
	// stream. Only the measured duration varies between process runs.
	v1bin := filepath.Join(dir, "tdtpcli_v1")
	if os.PathSeparator == '\\' {
		v1bin += ".exe"
	}
	buildV1 := exec.Command("go", "build", "-o", v1bin, "../tdtpcli")
	if output, err := buildV1.CombinedOutput(); err != nil {
		t.Fatalf("build v1 CLI: %v\n%s", err, output)
	}
	parityWF := filepath.Join(dir, "parity.yaml")
	parityYAML := fmt.Sprintf("name: parity\nsteps:\n"+
		"  - id: inspect\n    command: --inspect %q\n"+
		"  - id: verify\n    command: --test %q\n    depends_on: [inspect]\n",
		filepath.ToSlash(input), filepath.ToSlash(input))
	if err := os.WriteFile(parityWF, []byte(parityYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	v1 := exec.Command(v1bin, "--steps", parityWF)
	v1out, err := v1.Output()
	if err != nil {
		t.Fatalf("v1 workflow failed: %v", err)
	}
	v2out, _ := run("--steps", parityWF)
	duration := regexp.MustCompile(`all steps completed in [^\r\n]+`)
	integrityTime := regexp.MustCompile(`Integrity check passed \([^\r\n]+\)`)
	normalize := func(s string) string {
		s = duration.ReplaceAllString(s, "all steps completed in <duration>")
		return integrityTime.ReplaceAllString(s, "Integrity check passed (<duration>)")
	}
	if normalize(string(v1out)) != normalize(v2out) {
		t.Fatalf("v1/v2 workflow output differs:\nv1: %q\nv2: %q", v1out, v2out)
	}
	v1Quiet := exec.Command(v1bin, "--quiet", "--steps", parityWF)
	v1QuietOut, err := v1Quiet.Output()
	if err != nil {
		t.Fatalf("v1 quiet workflow failed: %v", err)
	}
	v2QuietOut, _ := run("--quiet", "--steps", parityWF)
	if normalize(string(v1QuietOut)) != normalize(v2QuietOut) {
		t.Fatalf("v1/v2 quiet workflow output differs:\nv1: %q\nv2: %q", v1QuietOut, v2QuietOut)
	}
}
