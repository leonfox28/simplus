//go:build !linux && !darwin

package control

import (
	"fmt"
	"net"
)

func peerUID(*net.UnixConn) (uint32, error) {
	return 0, fmt.Errorf("unix peer credentials are unsupported on this platform")
}
