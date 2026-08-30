//go:build !windows

package updater

import "syscall"

// replaceProcess swaps the running image for the new binary in place, so the
// PID survives and a supervisor never sees the service go away.
func replaceProcess(path string, args, env []string) error {
	return syscall.Exec(path, args, env)
}
