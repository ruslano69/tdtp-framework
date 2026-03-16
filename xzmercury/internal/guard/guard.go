// Package guard implements a startup privilege check.
//
// xzmercury must NOT run as root (Linux) or with elevated Administrator
// privileges (Windows). This prevents a local admin from attaching a debugger
// to the process and dumping AES keys from RAM.
//
// Note: this is a defense-in-depth measure. A process with CAP_SYS_PTRACE or
// kernel-level access can still inspect memory. The goal is to raise the bar,
// not provide an absolute guarantee.
package guard

// Check returns a non-nil error if the current process runs with excessive
// privileges. Call this at startup before doing anything else.
func Check() error {
	return checkPrivileges()
}
