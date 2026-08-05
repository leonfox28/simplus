//go:build linux

package qdc507sms

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

type unixSerialSession struct {
	fd       int
	original *unix.Termios
}

func openSerialSession(endpoint string) (serialSession, error) {
	fd, err := unix.Open(endpoint, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	closeOnError := func(err error) (serialSession, error) {
		_ = unix.Close(fd)
		return nil, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return closeOnError(fmt.Errorf("inspect tty: %w", err))
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFCHR {
		return closeOnError(errors.New("QDC507 SMS endpoint is not a character device"))
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return closeOnError(fmt.Errorf("lock tty: %w", err))
	}
	original, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		_ = unix.Flock(fd, unix.LOCK_UN)
		return closeOnError(fmt.Errorf("read tty configuration: %w", err))
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
		_ = unix.Flock(fd, unix.LOCK_UN)
		return closeOnError(fmt.Errorf("configure tty: %w", err))
	}
	return &unixSerialSession{fd: fd, original: original}, nil
}

func (session *unixSerialSession) FlushInput() error {
	if session == nil || session.fd < 0 {
		return errors.New("QDC507 SMS tty session is closed")
	}
	return unix.IoctlSetInt(session.fd, unix.TCFLSH, unix.TCIFLUSH)
}

func (session *unixSerialSession) Write(ctx context.Context, payload []byte, timeout time.Duration) error {
	if session == nil || session.fd < 0 {
		return errors.New("QDC507 SMS tty session is closed")
	}
	deadline := time.Now().Add(timeout)
	for len(payload) != 0 {
		if err := pollTTYContext(ctx, session.fd, unix.POLLOUT, time.Until(deadline)); err != nil {
			return err
		}
		written, err := unix.Write(session.fd, payload)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			return err
		}
		if written < 1 || written > len(payload) {
			return ioProgressError("write")
		}
		payload = payload[written:]
	}
	return nil
}

func (session *unixSerialSession) Read(ctx context.Context, buffer []byte, timeout time.Duration) (int, error) {
	if session == nil || session.fd < 0 {
		return 0, errors.New("QDC507 SMS tty session is closed")
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := pollTTYContext(ctx, session.fd, unix.POLLIN, time.Until(deadline)); err != nil {
			return 0, err
		}
		count, err := unix.Read(session.fd, buffer)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			return 0, err
		}
		if count == 0 {
			continue
		}
		return count, nil
	}
}

func (session *unixSerialSession) Close() error {
	if session == nil || session.fd < 0 {
		return nil
	}
	fd := session.fd
	session.fd = -1
	var joined error
	if session.original != nil {
		joined = errors.Join(joined, unix.IoctlSetTermios(fd, unix.TCSETS, session.original))
	}
	joined = errors.Join(joined, unix.Flock(fd, unix.LOCK_UN))
	joined = errors.Join(joined, unix.Close(fd))
	return joined
}

func pollTTYContext(ctx context.Context, fd int, events int16, timeout time.Duration) error {
	if timeout <= 0 {
		return context.DeadlineExceeded
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.DeadlineExceeded
		}
		milliseconds := int(remaining.Milliseconds())
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
			return errors.New("QDC507 SMS tty became unavailable")
		}
		if pollFD[0].Revents&events != 0 {
			return nil
		}
	}
}

func ioProgressError(operation string) error {
	return fmt.Errorf("QDC507 SMS tty %s made no progress", operation)
}
