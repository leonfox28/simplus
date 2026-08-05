package agentapi

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

type ListenerOptions struct {
	Path          string
	DirectoryMode os.FileMode
	SocketMode    os.FileMode
	OwnerUID      int
	OwnerGID      int
	AllowedUIDs   []uint32
}

type UIDListener struct {
	net.Listener
	path      string
	allowed   map[uint32]struct{}
	closeOnce sync.Once
	closeErr  error
}

func Listen(options ListenerOptions) (*UIDListener, error) {
	if !filepath.IsAbs(options.Path) {
		return nil, errors.New("agent socket path must be absolute")
	}
	if len(options.AllowedUIDs) == 0 {
		return nil, errors.New("agent socket requires at least one allowed uid")
	}
	if options.DirectoryMode == 0 {
		options.DirectoryMode = 0o700
	}
	if options.SocketMode == 0 {
		options.SocketMode = 0o600
	}
	if options.DirectoryMode.Perm()&0o027 != 0 || options.SocketMode.Perm()&0o007 != 0 {
		return nil, errors.New("agent socket permissions must not grant directory writes or socket access to other users")
	}
	path := filepath.Clean(options.Path)
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, options.DirectoryMode.Perm()); err != nil {
		return nil, fmt.Errorf("create agent socket directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("agent socket parent must be a real directory")
	}
	if err := os.Chmod(directory, options.DirectoryMode.Perm()); err != nil {
		return nil, fmt.Errorf("secure agent socket directory: %w", err)
	}
	if options.OwnerUID >= 0 || options.OwnerGID >= 0 {
		if err := os.Chown(directory, options.OwnerUID, options.OwnerGID); err != nil {
			return nil, fmt.Errorf("own agent socket directory: %w", err)
		}
	}
	if err := removeStaleAgentSocket(path); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on agent socket: %w", err)
	}
	cleanup := func(cause error) (*UIDListener, error) {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, cause
	}
	if err := os.Chmod(path, options.SocketMode.Perm()); err != nil {
		return cleanup(fmt.Errorf("secure agent socket: %w", err))
	}
	if options.OwnerUID >= 0 || options.OwnerGID >= 0 {
		if err := os.Chown(path, options.OwnerUID, options.OwnerGID); err != nil {
			return cleanup(fmt.Errorf("own agent socket: %w", err))
		}
	}
	allowed := make(map[uint32]struct{}, len(options.AllowedUIDs))
	for _, uid := range options.AllowedUIDs {
		allowed[uid] = struct{}{}
	}
	return &UIDListener{Listener: listener, path: path, allowed: allowed}, nil
}

func (listener *UIDListener) Accept() (net.Conn, error) {
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
		if err != nil {
			_ = connection.Close()
			continue
		}
		if _, ok := listener.allowed[uid]; !ok {
			_ = connection.Close()
			continue
		}
		return connection, nil
	}
}

func (listener *UIDListener) Close() error {
	listener.closeOnce.Do(func() {
		listener.closeErr = errors.Join(listener.Listener.Close(), removeOwnedAgentSocket(listener.path))
	})
	return listener.closeErr
}

func removeStaleAgentSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing agent socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("refusing to replace non-socket agent path")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("existing agent socket is not owned by this service uid")
	}
	connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return errors.New("another agent listener is already active")
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
		return fmt.Errorf("probe existing agent socket: %w", dialErr)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale agent socket: %w", err)
	}
	return nil
}

func removeOwnedAgentSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("agent socket path changed before cleanup")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("agent socket owner changed before cleanup")
	}
	return os.Remove(path)
}
