//go:build linux || darwin

package sqlite

import (
	"os"
	"syscall"
	"testing"
)

func TestMain(main *testing.M) {
	// testing.T.TempDir creates numbered children with 0777 masked by the process
	// umask. Keep security-boundary fixtures deterministic on developer hosts
	// whose interactive umask permits group writes.
	previous := syscall.Umask(0o022)
	code := main.Run()
	syscall.Umask(previous)
	os.Exit(code)
}
