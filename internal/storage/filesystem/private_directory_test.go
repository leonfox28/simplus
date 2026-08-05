package filesystem

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPreparePrivateDirectoryCreatesAndRevalidatesStableIdentity(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "recordings")
	first, err := PreparePrivateDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PreparePrivateDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == "" || first.Device == 0 || first.Inode == 0 || first != second {
		t.Fatalf("identities = %#v, %#v", first, second)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %04o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("write probe was not cleaned up: %#v", entries)
	}
}

func TestPreparePrivateDirectoryRejectsRelativeSymlinkAndUnsafeMode(t *testing.T) {
	if _, err := PreparePrivateDirectory("relative/recordings"); err == nil {
		t.Fatal("relative path was accepted")
	}
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if _, err := PreparePrivateDirectory(link); err == nil {
		t.Fatal("symlink directory was accepted")
	}

	unsafe := filepath.Join(parent, "unsafe")
	if err := os.Mkdir(unsafe, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := PreparePrivateDirectory(unsafe); err == nil || !strings.Contains(err.Error(), "0700") {
		t.Fatalf("unsafe mode error = %v", err)
	}
}
