package control

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const socketFilename = "simplusd-control.sock"

func SocketPath(dataRoot string) string {
	return filepath.Join(dataRoot, "run", socketFilename)
}

type RootListener struct {
	net.Listener
	path       string
	allowedUID uint32
	closeOnce  sync.Once
	closeErr   error
}

func ListenRootOnly(path string, allowedUID uint32) (*RootListener, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("control socket path must be absolute")
	}
	path = filepath.Clean(path)
	directory := filepath.Dir(path)
	if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create control socket directory: %w", err)
	}
	if err := validatePrivateDirectory(directory); err != nil {
		return nil, err
	}
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on control socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("secure control socket: %w", err)
	}
	return &RootListener{Listener: listener, path: path, allowedUID: allowedUID}, nil
}

func (listener *RootListener) Accept() (net.Conn, error) {
	for {
		connection, err := listener.Listener.Accept()
		if err != nil {
			return nil, err
		}
		unixConnection, ok := connection.(*net.UnixConn)
		if !ok {
			_ = connection.Close()
			continue
		}
		uid, err := peerUID(unixConnection)
		if err != nil || uid != listener.allowedUID {
			_ = connection.Close()
			continue
		}
		return connection, nil
	}
}

func (listener *RootListener) Close() error {
	listener.closeOnce.Do(func() {
		listener.closeErr = errors.Join(listener.Listener.Close(), removeOwnedSocket(listener.path))
	})
	return listener.closeErr
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect control socket directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("control socket parent must be a real directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("control socket parent must be owned by the service uid")
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("control socket parent permissions must be 0700, found %04o", info.Mode().Perm())
	}
	return nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing control socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket control path")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("existing control socket is not owned by the service uid")
	}
	connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return fmt.Errorf("another control listener is already active")
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
		return fmt.Errorf("probe existing control socket: %w", dialErr)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale control socket: %w", err)
	}
	return nil
}

func removeOwnedSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("control path changed before cleanup")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("control socket owner changed before cleanup")
	}
	return os.Remove(path)
}
