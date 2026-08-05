package filesystem

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type DirectoryIdentity struct {
	Path   string
	Device uint64
	Inode  uint64
}

func PreparePrivateDirectory(path string) (DirectoryIdentity, error) {
	if path == "" {
		return DirectoryIdentity{}, errors.New("private directory path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return DirectoryIdentity{}, fmt.Errorf("resolve private directory path: %w", err)
	}
	if filepath.Clean(path) != absolute {
		return DirectoryIdentity{}, errors.New("private directory path must be absolute and clean")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return DirectoryIdentity{}, fmt.Errorf("resolve private directory parent: %w", err)
	}
	if err := validateAncestorChain(parent); err != nil {
		return DirectoryIdentity{}, err
	}
	canonical := filepath.Join(parent, filepath.Base(absolute))
	if _, err := os.Lstat(canonical); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(canonical, 0o700); err != nil {
			return DirectoryIdentity{}, fmt.Errorf("create private directory: %w", err)
		}
	} else if err != nil {
		return DirectoryIdentity{}, fmt.Errorf("inspect private directory: %w", err)
	}

	identity, err := validatePrivateDirectory(canonical)
	if err != nil {
		return DirectoryIdentity{}, err
	}
	if err := writeProbe(canonical); err != nil {
		return DirectoryIdentity{}, err
	}
	identityAfter, err := validatePrivateDirectory(canonical)
	if err != nil {
		return DirectoryIdentity{}, err
	}
	if identity.Device != identityAfter.Device || identity.Inode != identityAfter.Inode {
		return DirectoryIdentity{}, errors.New("private directory identity changed while being validated")
	}
	return identityAfter, nil
}

func validatePrivateDirectory(path string) (DirectoryIdentity, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return DirectoryIdentity{}, fmt.Errorf("stat private directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return DirectoryIdentity{}, errors.New("private directory must be a real directory, not a symlink or file")
	}
	if info.Mode().Perm() != 0o700 {
		return DirectoryIdentity{}, fmt.Errorf("private directory permissions must be 0700, found %04o", info.Mode().Perm())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return DirectoryIdentity{}, errors.New("private directory identity is unavailable")
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return DirectoryIdentity{}, fmt.Errorf("private directory owner uid %d does not match service uid %d", stat.Uid, os.Geteuid())
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return DirectoryIdentity{}, fmt.Errorf("resolve private directory: %w", err)
	}
	if resolved != path {
		return DirectoryIdentity{}, errors.New("private directory resolved through a symlink")
	}
	return DirectoryIdentity{Path: path, Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}, nil
}

func validateAncestorChain(path string) error {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("stat private directory ancestor %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("private directory ancestor is not a real directory: %s", current)
		}
		if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("private directory ancestor is writable by group or other users without sticky protection: %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func writeProbe(directory string) error {
	file, err := os.OpenFile(filepath.Join(directory, ".simplus-write-probe"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create private directory write probe: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := io.WriteString(file, "simplus-private-directory-v1\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("write private directory probe: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync private directory probe: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close private directory probe: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove private directory probe: %w", err)
	}
	return nil
}
