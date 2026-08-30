//go:build windows

package updater

import "os"

// replaceProcess has no Windows equivalent of exec(2); the real Windows path
// is the PowerShell hand-off in applyUpdateWindows, and this exists so the
// package still builds.
func replaceProcess(path string, args, env []string) error {
	proc, err := os.StartProcess(path, args, &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Env:   env,
	})
	if err != nil {
		return err
	}
	_ = proc.Release()
	os.Exit(0)
	return nil
}
