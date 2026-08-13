package qdc507sms

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
)

const (
	maxTTYResponseBytes = maxTranscriptLines * (maxTranscriptLine + 2)
	maxTTYCommandBytes  = 64
	maxTTYSubmitBytes   = 513 // 512 hexadecimal PDU characters plus Ctrl-Z.
	maxTTYReadChunk     = 4096
)

var (
	ErrTTYUnsupported       = errors.New("QDC507 SMS tty transport is unsupported on this platform")
	ErrPromptNotDispatched  = errors.New("QDC507 SMS payload was not dispatched")
	ErrTTYResponseTooLarge  = errors.New("QDC507 SMS tty response exceeds its bounded size")
	ErrTTYTranscriptInvalid = errors.New("QDC507 SMS tty transcript is invalid")
)

type serialSession interface {
	FlushInput() error
	Write(context.Context, []byte, time.Duration) error
	Read(context.Context, []byte, time.Duration) (int, error)
	Close() error
}

type serialSessionOpener func(string) (serialSession, error)

// TTYTransport implements the transcript boundary over one bounded,
// exclusively opened serial session per AT exchange. It is inert until an
// explicitly assembled Adapter calls it; constructing it never opens a tty.
type TTYTransport struct {
	open serialSessionOpener
}

var _ Transport = (*TTYTransport)(nil)

func NewTTYTransport() *TTYTransport {
	return &TTYTransport{open: openSerialSession}
}

func (transport *TTYTransport) Command(ctx context.Context, endpoint, command string, timeout time.Duration) ([]string, error) {
	if transport == nil {
		return nil, ErrTTYUnsupported
	}
	return ttyCommand(ctx, transport.open, endpoint, command, timeout)
}

func ttyCommand(ctx context.Context, open serialSessionOpener, endpoint, command string, timeout time.Duration) ([]string, error) {
	if err := validateTTYExchange(endpoint, command, timeout); err != nil {
		return nil, err
	}
	session, err := openTTYSession(open, endpoint)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	if err := session.FlushInput(); err != nil {
		return nil, fmt.Errorf("flush QDC507 SMS tty input: %w", err)
	}
	deadline := time.Now().Add(timeout)
	if err := session.Write(ctx, []byte(command+"\r"), remainingTTYTimeout(deadline)); err != nil {
		return nil, fmt.Errorf("write QDC507 SMS AT command: %w", err)
	}
	return readTTYTerminal(ctx, session, command, "", deadline, nil)
}

func (transport *TTYTransport) Prompt(ctx context.Context, endpoint, command string, payload []byte, timeout time.Duration) ([]string, error) {
	if transport == nil {
		return nil, errors.Join(ErrPromptNotDispatched, ErrTTYUnsupported)
	}
	return ttyPrompt(ctx, transport.open, endpoint, command, payload, timeout)
}

func ttyPrompt(ctx context.Context, open serialSessionOpener, endpoint, command string, payload []byte, timeout time.Duration) ([]string, error) {
	if err := validateTTYExchange(endpoint, command, timeout); err != nil {
		return nil, errors.Join(ErrPromptNotDispatched, err)
	}
	if err := validateSubmitPayload(payload); err != nil {
		return nil, errors.Join(ErrPromptNotDispatched, err)
	}
	session, err := openTTYSession(open, endpoint)
	if err != nil {
		return nil, errors.Join(ErrPromptNotDispatched, err)
	}
	defer session.Close()
	if err := session.FlushInput(); err != nil {
		return nil, errors.Join(ErrPromptNotDispatched, fmt.Errorf("flush QDC507 SMS tty input: %w", err))
	}
	deadline := time.Now().Add(timeout)
	if err := session.Write(ctx, []byte(command+"\r"), remainingTTYTimeout(deadline)); err != nil {
		return nil, errors.Join(ErrPromptNotDispatched, fmt.Errorf("write QDC507 SMS prompt command: %w", err))
	}
	prefix, terminal, err := readTTYPrompt(ctx, session, command, deadline)
	if err != nil {
		return nil, errors.Join(ErrPromptNotDispatched, err)
	}
	if terminal != nil {
		return terminal, nil
	}
	if err := session.Write(ctx, payload, remainingTTYTimeout(deadline)); err != nil {
		return nil, fmt.Errorf("write QDC507 SMS submit payload: %w", err)
	}
	payloadEcho := string(payload[:len(payload)-1])
	return readTTYTerminal(ctx, session, "", payloadEcho, deadline, prefix)
}

func (transport *TTYTransport) openSession(endpoint string) (serialSession, error) {
	if transport == nil {
		return nil, ErrTTYUnsupported
	}
	return openTTYSession(transport.open, endpoint)
}

func openTTYSession(open serialSessionOpener, endpoint string) (serialSession, error) {
	if open == nil {
		return nil, ErrTTYUnsupported
	}
	session, err := open(endpoint)
	if err != nil {
		return nil, fmt.Errorf("open QDC507 SMS tty: %w", err)
	}
	if session == nil {
		return nil, errors.New("QDC507 SMS tty opener returned no session")
	}
	return session, nil
}

func validateTTYExchange(endpoint, command string, timeout time.Duration) error {
	if endpoint == "" || len(endpoint) > 4096 || strings.IndexFunc(endpoint, unicode.IsControl) >= 0 {
		return errors.New("QDC507 SMS tty endpoint is invalid")
	}
	if len(command) < 2 || len(command) > maxTTYCommandBytes || !strings.HasPrefix(command, "AT") {
		return errors.New("QDC507 SMS AT command is invalid")
	}
	for _, character := range command {
		if character < 0x20 || character > 0x7e {
			return errors.New("QDC507 SMS AT command contains a non-printable byte")
		}
	}
	if timeout <= 0 || timeout > sendTimeout {
		return errors.New("QDC507 SMS tty timeout is outside its bounded range")
	}
	return nil
}

func validateSubmitPayload(payload []byte) error {
	if len(payload) < 3 || len(payload) > maxTTYSubmitBytes || payload[len(payload)-1] != 0x1a || (len(payload)-1)%2 != 0 {
		return errors.New("QDC507 SMS submit payload is invalid")
	}
	for _, value := range payload[:len(payload)-1] {
		if (value < '0' || value > '9') && (value < 'A' || value > 'F') {
			return errors.New("QDC507 SMS submit payload is not uppercase hexadecimal PDU data")
		}
	}
	return nil
}

func readTTYPrompt(ctx context.Context, session serialSession, command string, deadline time.Time) ([]byte, []string, error) {
	buffer := make([]byte, 0, 128)
	for {
		chunk, err := readTTYChunk(ctx, session, deadline)
		if err != nil {
			return nil, nil, fmt.Errorf("wait for QDC507 SMS submit prompt: %w", err)
		}
		buffer, err = appendTTYBounded(buffer, chunk)
		if err != nil {
			return nil, nil, err
		}
		if lines, ok := ttyTerminalLines(buffer, command, ""); ok {
			return nil, lines, nil
		}
		if prompt := findTTYPrompt(buffer); prompt >= 0 {
			remaining := append([]byte(nil), buffer[prompt+1:]...)
			if len(remaining) != 0 && remaining[0] == ' ' {
				remaining = remaining[1:]
			}
			return remaining, nil, nil
		}
	}
}

func readTTYTerminal(ctx context.Context, session serialSession, command, payloadEcho string, deadline time.Time, initial []byte) ([]string, error) {
	buffer := append([]byte(nil), initial...)
	if len(buffer) > maxTTYResponseBytes {
		return nil, ErrTTYResponseTooLarge
	}
	for {
		if lines, ok := ttyTerminalLines(buffer, command, payloadEcho); ok {
			return lines, nil
		}
		chunk, err := readTTYChunk(ctx, session, deadline)
		if err != nil {
			return nil, fmt.Errorf("read QDC507 SMS tty response: %w", err)
		}
		buffer, err = appendTTYBounded(buffer, chunk)
		if err != nil {
			return nil, err
		}
	}
}

func readTTYChunk(ctx context.Context, session serialSession, deadline time.Time) ([]byte, error) {
	remaining := remainingTTYTimeout(deadline)
	if remaining <= 0 {
		return nil, context.DeadlineExceeded
	}
	buffer := make([]byte, maxTTYReadChunk)
	count, err := session.Read(ctx, buffer, remaining)
	if err != nil {
		return nil, err
	}
	if count < 0 || count > len(buffer) {
		return nil, ErrTTYTranscriptInvalid
	}
	if count == 0 {
		return nil, io.ErrNoProgress
	}
	return buffer[:count], nil
}

func appendTTYBounded(buffer, chunk []byte) ([]byte, error) {
	if len(chunk) > maxTTYResponseBytes-len(buffer) {
		return nil, ErrTTYResponseTooLarge
	}
	return append(buffer, chunk...), nil
}

func ttyTerminalLines(buffer []byte, command, payloadEcho string) ([]string, bool) {
	lines := splitTTYLines(buffer, command, payloadEcho)
	for index, line := range lines {
		if line == "OK" || line == "ERROR" || strings.HasPrefix(line, "+CME ERROR:") || strings.HasPrefix(line, "+CMS ERROR:") {
			return lines[:index+1], true
		}
	}
	return nil, false
}

func splitTTYLines(buffer []byte, command, payloadEcho string) []string {
	text := strings.ReplaceAll(string(buffer), "\r", "\n")
	parts := strings.Split(text, "\n")
	lines := make([]string, 0, len(parts))
	payloadEchoSkipped := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == command {
			continue
		}
		if !payloadEchoSkipped && payloadEcho != "" && strings.TrimSuffix(part, string(rune(0x1a))) == payloadEcho {
			payloadEchoSkipped = true
			continue
		}
		lines = append(lines, part)
	}
	return lines
}

func findTTYPrompt(buffer []byte) int {
	for index, value := range buffer {
		if value != '>' {
			continue
		}
		if index == 0 || buffer[index-1] == '\r' || buffer[index-1] == '\n' {
			return index
		}
	}
	return -1
}

func remainingTTYTimeout(deadline time.Time) time.Duration {
	return time.Until(deadline)
}
