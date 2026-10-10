//go:build integration && !windows

package integration

import (
	"os/exec"
	"syscall"
)

// setProcGroup puts the child and everything it starts into its own process
// group, so killProcGroup reaches the binary "go run" compiled, not only
// "go run" itself.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcGroup(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
