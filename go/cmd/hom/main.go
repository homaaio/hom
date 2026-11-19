// hom: a package manager for any OS. This file only connects the process (arguments, terminal,
// Ctrl+C) to package app, where the commands are.
package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/homaaio/hom/pkg/app"
)

// version is set by the release build: -ldflags "-X main.version=2.0.0".
var version = "2.0.0-dev"

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
			exe = resolved
		}
		exeDir = filepath.Dir(exe)
	}
	e := &app.Env{
		Version: version,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		TTY:     isTerminal(os.Stdin),
		OutTTY:  isTerminal(os.Stdout),
		ExeDir:  exeDir,
	}
	code := e.Run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}
