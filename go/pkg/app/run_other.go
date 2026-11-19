//go:build !windows

package app

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// canRun says whether hom knows how to start this kind of installer on this OS (spec 1.6).
func canRun(path string) bool {
	p := strings.ToLower(path)
	switch runtime.GOOS {
	case "linux":
		return strings.HasSuffix(p, ".appimage")
	case "darwin":
		return strings.HasSuffix(p, ".dmg") || strings.HasSuffix(p, ".pkg")
	}
	return false
}

// runProgram starts an installer and waits for it. It returns the exit code of the program.
// args are the arguments of the index entry; they are only used for programs that take them.
func runProgram(path, args string) (int, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		if err := os.Chmod(path, 0o755); err != nil {
			return 0, err
		}
		cmd = exec.Command(path, strings.Fields(args)...)
	case "darwin":
		cmd = exec.Command("open", "-W", path)
	default:
		return 0, errors.New("this kind of file can't be started on this OS")
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return 0, err
	}
	return 0, nil
}
