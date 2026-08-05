//go:build linux || darwin

package httpapi

import (
	"os"
	"syscall"
	"testing"
)

func TestMain(main *testing.M) {
	// HTTP integration tests create real secured SQLite roots below TempDir.
	// Normalize fixture ancestors even when the invoking user's umask is 0002.
	previous := syscall.Umask(0o022)
	code := main.Run()
	syscall.Umask(previous)
	os.Exit(code)
}
