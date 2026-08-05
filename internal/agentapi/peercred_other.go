//go:build !linux && !darwin

package agentapi

import (
	"errors"
	"net"
)

func peerUID(*net.UnixConn) (uint32, error) {
	return 0, errors.New("unix peer credentials are unsupported on this platform")
}
