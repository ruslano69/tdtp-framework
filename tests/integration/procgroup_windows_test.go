//go:build integration && windows

package integration

import (
	"os/exec"
	"strconv"
)

// Windows has no process groups in the POSIX sense (SysProcAttr has no
// Setpgid, which is why this package did not compile here). taskkill /T
// ends the whole tree under "go run", including the binary it compiled.
func setProcGroup(*exec.Cmd) {}

func killProcGroup(cmd *exec.Cmd) {
	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
