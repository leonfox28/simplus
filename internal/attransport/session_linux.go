//go:build linux

package attransport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/sys/unix"
)

const (
	maximumCommandLength = 1024
	maximumResponseSize  = 8192
)

type platformOpener struct{}

type session struct {
	fd       int
	original *unix.Termios
}

func newPlatformOpener() Opener { return platformOpener{} }

func (platformOpener) Open(endpoint string) (Session, error) {
	if !filepath.IsAbs(endpoint) || endpoint == string(filepath.Separator) {
		return nil, &OpenError{Kind: OpenUnavailable, cause: errors.New("AT endpoint path is invalid")}
	}
	if deviceOpenByAnotherProcess(endpoint) {
		return nil, &OpenError{Kind: OpenBusy, Retryable: true, cause: errors.New("AT endpoint is already open")}
	}
	fd, err := unix.Open(endpoint, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
			return nil, &OpenError{Kind: OpenPermission, cause: err}
		}
		return nil, &OpenError{Kind: OpenUnavailable, Retryable: true, cause: err}
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(fd)
		return nil, &OpenError{Kind: OpenBusy, Retryable: true, cause: err}
	}
	original, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = unix.Close(fd)
		return nil, &OpenError{Kind: OpenConfigure, Retryable: true, cause: err}
	}
	configured := *original
	configured.Iflag = unix.IGNPAR
	configured.Oflag = 0
	configured.Cflag = unix.B115200 | unix.CS8 | unix.CREAD | unix.CLOCAL
	configured.Lflag = 0
	configured.Cc[unix.VMIN] = 0
	configured.Cc[unix.VTIME] = 0
	configured.Ispeed = unix.B115200
	configured.Ospeed = unix.B115200
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &configured); err != nil {
		_ = unix.IoctlSetTermios(fd, unix.TCSETS, original)
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = unix.Close(fd)
		return nil, &OpenError{Kind: OpenConfigure, Retryable: true, cause: err}
	}
	_ = unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIOFLUSH)
	return &session{fd: fd, original: original}, nil
}

func (current *session) Query(ctx context.Context, command string, timeout time.Duration) ([]string, error) {
	if current == nil || current.fd < 0 {
		return nil, errors.New("AT session is closed")
	}
	if command == "" || len(command) > maximumCommandLength || strings.ContainsAny(command, "\r\n") {
		return nil, errors.New("invalid bounded AT query")
	}
	_ = unix.IoctlSetInt(current.fd, unix.TCFLSH, unix.TCIFLUSH)
	wirePayload := []byte(command + "\r")
	defer zero(wirePayload)
	for payload := wirePayload; len(payload) != 0; {
		if err := pollContext(ctx, current.fd, unix.POLLOUT, timeout); err != nil {
			return nil, err
		}
		written, err := unix.Write(current.fd, payload)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			return nil, err
		}
		payload = payload[written:]
	}
	deadline := time.Now().Add(timeout)
	buffer := make([]byte, 0, 2048)
	temporary := make([]byte, 512)
	defer func() { zero(buffer) }()
	defer zero(temporary)
	for time.Now().Before(deadline) {
		if err := pollContext(ctx, current.fd, unix.POLLIN, time.Until(deadline)); err != nil {
			return nil, err
		}
		count, err := unix.Read(current.fd, temporary)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			return nil, err
		}
		if count == 0 {
			continue
		}
		if len(buffer)+count > maximumResponseSize {
			return nil, errors.New("AT response exceeds bounded size")
		}
		buffer = append(buffer, temporary[:count]...)
		lines := splitLines(string(buffer), command)
		if HasTerminalResponse(lines) {
			return lines, nil
		}
	}
	return nil, errors.New("AT query timed out")
}

func (current *session) Close() {
	if current == nil || current.fd < 0 {
		return
	}
	if current.original != nil {
		_ = unix.IoctlSetTermios(current.fd, unix.TCSETS, current.original)
	}
	_ = unix.Flock(current.fd, unix.LOCK_UN)
	_ = unix.Close(current.fd)
	current.fd = -1
}

func pollContext(ctx context.Context, fd int, events int16, timeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("poll timed out")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		milliseconds := int(time.Until(deadline).Milliseconds())
		if milliseconds < 1 {
			milliseconds = 1
		}
		if milliseconds > 200 {
			milliseconds = 200
		}
		pollFD := []unix.PollFd{{Fd: int32(fd), Events: events}}
		count, err := unix.Poll(pollFD, milliseconds)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return err
		}
		if count == 0 {
			continue
		}
		if pollFD[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return errors.New("AT endpoint became unavailable")
		}
		if pollFD[0].Revents&events != 0 {
			return nil
		}
	}
	return errors.New("poll timed out")
}

func splitLines(response, command string) []string {
	response = strings.ReplaceAll(response, "\r", "\n")
	parts := strings.Split(response, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == command {
			continue
		}
		lines = append(lines, safeText(part, maximumResponseSize))
	}
	return lines
}

func safeText(value string, limit int) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func deviceOpenByAnotherProcess(path string) bool {
	target, err := os.Stat(path)
	if err != nil {
		return false
	}
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	self := os.Getpid()
	for _, process := range processes {
		pid, err := strconv.Atoi(process.Name())
		if err != nil || pid == self {
			continue
		}
		fds, err := os.ReadDir(filepath.Join("/proc", process.Name(), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			candidate, err := os.Stat(filepath.Join("/proc", process.Name(), "fd", fd.Name()))
			if err == nil && os.SameFile(target, candidate) {
				return true
			}
		}
	}
	return false
}
