//go:build linux || darwin

package resourcelease

import (
	"os"
	"syscall"
	"testing"
)

func TestMain(main *testing.M) {
	// Resource-lease integration tests create secured runtime databases below
	// TempDir. Normalize fixture ancestors for developer umasks such as 0002.
	previous := syscall.Umask(0o022)
	code := main.Run()
	syscall.Umask(previous)
	os.Exit(code)
}
