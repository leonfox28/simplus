//go:build darwin

package agentapi

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func peerUID(connection *net.UnixConn) (uint32, error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid uint32
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err != nil {
			controlErr = err
			return
		}
		uid = credentials.Uid
	}); err != nil {
		return 0, err
	}
	if controlErr != nil {
		return 0, fmt.Errorf("read unix peer credentials: %w", controlErr)
	}
	return uid, nil
}
