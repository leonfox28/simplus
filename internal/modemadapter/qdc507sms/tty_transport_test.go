package qdc507sms

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

type serialRead struct {
	data        []byte
	err         error
	waitContext bool
}

type scriptedSerialSession struct {
	reads   []serialRead
	writes  [][]byte
	flushes int
	closed  bool
}

func (session *scriptedSerialSession) FlushInput() error {
	session.flushes++
	return nil
}

func (session *scriptedSerialSession) Write(_ context.Context, payload []byte, _ time.Duration) error {
	session.writes = append(session.writes, append([]byte(nil), payload...))
	return nil
}

func (session *scriptedSerialSession) Read(ctx context.Context, buffer []byte, _ time.Duration) (int, error) {
	if len(session.reads) == 0 {
		return 0, io.EOF
	}
	read := &session.reads[0]
	if read.waitContext {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	if read.err != nil {
		err := read.err
		session.reads = session.reads[1:]
		return 0, err
	}
	count := copy(buffer, read.data)
	read.data = read.data[count:]
	if len(read.data) == 0 {
		session.reads = session.reads[1:]
	}
	return count, nil
}

func (session *scriptedSerialSession) Close() error {
	session.closed = true
	return nil
}

func fixtureTTYTransport(t *testing.T, session *scriptedSerialSession) *TTYTransport {
	t.Helper()
	opened := false
	transport := &TTYTransport{open: func(endpoint string) (serialSession, error) {
		if endpoint != "/dev/ttyUSB2" {
			t.Fatalf("opened endpoint %q", endpoint)
		}
		if opened {
			t.Fatal("serial session opened more than once for one exchange")
		}
		opened = true
		return session, nil
	}}
	t.Cleanup(func() {
		if !opened {
			t.Error("serial session was not opened")
		}
	})
	return transport
}

func TestTTYTransportReadsFragmentedCommandTranscript(t *testing.T) {
	session := &scriptedSerialSession{reads: []serialRead{
		{data: []byte("AT+CMGL=4\r\r\n+CMGL: 3,0,,24\r\n08916831")},
		{data: []byte("08200105F0040D91685120012194F600F10180817144302304F4F29C0E\r\n")},
		{data: []byte("OK\r\n+UNSOLICITED: ignored-after-terminal\r\n")},
	}}
	lines, err := fixtureTTYTransport(t, session).Command(t.Context(), "/dev/ttyUSB2", "AT+CMGL=4", listTimeout)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"+CMGL: 3,0,,24",
		"0891683108200105F0040D91685120012194F600F10180817144302304F4F29C0E",
		"OK",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %#v", lines)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Fatalf("line %d = %q, want %q", index, lines[index], want[index])
		}
	}
	if len(session.writes) != 1 || !bytes.Equal(session.writes[0], []byte("AT+CMGL=4\r")) || session.flushes != 1 || !session.closed {
		t.Fatalf("session writes=%q flushes=%d closed=%t", session.writes, session.flushes, session.closed)
	}
}

func TestTTYTransportWaitsForPromptBeforeDispatchingPayload(t *testing.T) {
	payload := append([]byte("0001000D91683108108300F0000005E8329BFD06"), 0x1a)
	session := &scriptedSerialSession{reads: []serialRead{
		{data: []byte("AT+CMGS=19\r\n")},
		{data: []byte("\r\n> ")},
		{data: []byte("\r\n+CMGS: 42\r\n")},
		{data: []byte("OK\r\n")},
	}}
	lines, err := fixtureTTYTransport(t, session).Prompt(t.Context(), "/dev/ttyUSB2", "AT+CMGS=19", payload, sendTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "+CMGS: 42" || lines[1] != "OK" {
		t.Fatalf("lines = %#v", lines)
	}
	if len(session.writes) != 2 || !bytes.Equal(session.writes[0], []byte("AT+CMGS=19\r")) || !bytes.Equal(session.writes[1], payload) {
		t.Fatalf("writes = %q", session.writes)
	}
}

func TestTTYTransportFiltersOneExactSubmitPayloadEcho(t *testing.T) {
	payloadText := "0001000D91683108108300F0000005E8329BFD06"
	payload := append([]byte(payloadText), 0x1a)
	for name, echo := range map[string]string{
		"without control-z": payloadText,
		"with control-z":    payloadText + string(rune(0x1a)),
	} {
		t.Run(name, func(t *testing.T) {
			session := &scriptedSerialSession{reads: []serialRead{
				{data: []byte("\r\n> ")},
				{data: []byte("\r\n" + echo + "\r\n+CMGS: 42\r\nOK\r\n")},
			}}
			lines, err := fixtureTTYTransport(t, session).Prompt(t.Context(), "/dev/ttyUSB2", "AT+CMGS=19", payload, sendTimeout)
			if err != nil || !reflect.DeepEqual(lines, []string{"+CMGS: 42", "OK"}) {
				t.Fatalf("lines=%#v error=%v", lines, err)
			}
		})
	}
}

func TestTTYTransportDoesNotHideUnexpectedOrDuplicateSubmitPayloadLines(t *testing.T) {
	payloadText := "0001000D91683108108300F0000005E8329BFD06"
	payload := append([]byte(payloadText), 0x1a)
	for name, response := range map[string]string{
		"different hex":    "0001000D91683108108300F0000005E8329BFD07\r\n+CMGS: 42\r\nOK\r\n",
		"duplicate echo":   payloadText + "\r\n" + payloadText + "\r\n+CMGS: 42\r\nOK\r\n",
		"unsolicited line": payloadText + "\r\n+PRIVATE-URC: bounded\r\n+CMGS: 42\r\nOK\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			session := &scriptedSerialSession{reads: []serialRead{{data: []byte("\r\n> ")}, {data: []byte("\r\n" + response)}}}
			lines, err := fixtureTTYTransport(t, session).Prompt(t.Context(), "/dev/ttyUSB2", "AT+CMGS=19", payload, sendTimeout)
			if err != nil || len(lines) != 3 || lines[1] != "+CMGS: 42" || lines[2] != "OK" {
				t.Fatalf("lines=%#v error=%v", lines, err)
			}
			if lines[0] == payloadText && name != "duplicate echo" {
				t.Fatalf("unexpected line was hidden as a payload echo: %#v", lines)
			}
		})
	}
}

func TestDriverConfirmsSubmitWhenTTYEchoesExactPayload(t *testing.T) {
	payloadText := "0001000D91683108108300F0000005E8329BFD06"
	sessions := []*scriptedSerialSession{
		{reads: []serialRead{{data: []byte("AT+CMGF=0\r\nOK\r\n")}}},
		{reads: []serialRead{{data: []byte("AT+CPMS=\"SM\",\"SM\",\"SM\"\r\n+CPMS: 0,10,0,10,0,10\r\nOK\r\n")}}},
		{reads: []serialRead{
			{data: []byte("AT+CMGS=19\r\n\r\n> ")},
			{data: []byte("\r\n" + payloadText + string(rune(0x1a)) + "\r\n+CMGS: 42\r\nOK\r\n")},
		}},
	}
	opened := 0
	transport := &TTYTransport{open: func(endpoint string) (serialSession, error) {
		if endpoint != "/dev/ttyUSB2" || opened >= len(sessions) {
			t.Fatalf("open endpoint=%q count=%d", endpoint, opened)
		}
		session := sessions[opened]
		opened++
		return session, nil
	}}
	driver, err := NewDriver(transport)
	if err != nil {
		t.Fatal(err)
	}
	result, err := driver.Send(t.Context(), qdc507Device(), "+8613800138000", "hello")
	if err != nil || len(result.Parts) != 1 || result.Parts[0].MessageReference != 42 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if opened != len(sessions) {
		t.Fatalf("opened %d sessions", opened)
	}
	for index, session := range sessions {
		if !session.closed || session.flushes != 1 {
			t.Fatalf("session %d closed=%t flushes=%d", index, session.closed, session.flushes)
		}
	}
}

func TestDriverKeepsDuplicateOrUnexpectedSubmitEchoOutcomeUnknown(t *testing.T) {
	payloadText := "0001000D91683108108300F0000005E8329BFD06"
	for name, response := range map[string]string{
		"duplicate":   payloadText + "\r\n" + payloadText + "\r\n+CMGS: 42\r\nOK\r\n",
		"different":   "0001000D91683108108300F0000005E8329BFD07\r\n+CMGS: 42\r\nOK\r\n",
		"unsolicited": payloadText + "\r\n+PRIVATE-URC: bounded\r\n+CMGS: 42\r\nOK\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			sessions := []*scriptedSerialSession{
				{reads: []serialRead{{data: []byte("AT+CMGF=0\r\nOK\r\n")}}},
				{reads: []serialRead{{data: []byte("AT+CPMS=\"SM\",\"SM\",\"SM\"\r\n+CPMS: 0,10,0,10,0,10\r\nOK\r\n")}}},
				{reads: []serialRead{{data: []byte("AT+CMGS=19\r\n\r\n> ")}, {data: []byte("\r\n" + response)}}},
			}
			opened := 0
			transport := &TTYTransport{open: func(string) (serialSession, error) {
				session := sessions[opened]
				opened++
				return session, nil
			}}
			driver, err := NewDriver(transport)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := driver.Send(t.Context(), qdc507Device(), "+8613800138000", "hello"); !errors.Is(err, ErrSendOutcomeUnknown) {
				t.Fatalf("send error=%v", err)
			}
		})
	}
}

func TestTTYTransportReturnsRejectionBeforePromptWithoutPayload(t *testing.T) {
	payload := append([]byte("0001000D91683108108300F0000005E8329BFD06"), 0x1a)
	session := &scriptedSerialSession{reads: []serialRead{{data: []byte("AT+CMGS=19\r\n+CMS ERROR: 302\r\n")}}}
	lines, err := fixtureTTYTransport(t, session).Prompt(t.Context(), "/dev/ttyUSB2", "AT+CMGS=19", payload, sendTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "+CMS ERROR: 302" || len(session.writes) != 1 {
		t.Fatalf("lines = %#v, writes = %q", lines, session.writes)
	}
}

func TestTTYTransportDistinguishesPreAndPostDispatchFailures(t *testing.T) {
	payload := append([]byte("0001000D91683108108300F0000005E8329BFD06"), 0x1a)
	before := &scriptedSerialSession{reads: []serialRead{{err: io.ErrUnexpectedEOF}}}
	_, err := fixtureTTYTransport(t, before).Prompt(t.Context(), "/dev/ttyUSB2", "AT+CMGS=19", payload, sendTimeout)
	if !errors.Is(err, ErrPromptNotDispatched) || !errors.Is(err, io.ErrUnexpectedEOF) || len(before.writes) != 1 {
		t.Fatalf("pre-dispatch error = %v, writes = %q", err, before.writes)
	}

	after := &scriptedSerialSession{reads: []serialRead{{data: []byte("\r\n> ")}, {err: io.ErrUnexpectedEOF}}}
	_, err = fixtureTTYTransport(t, after).Prompt(t.Context(), "/dev/ttyUSB2", "AT+CMGS=19", payload, sendTimeout)
	if err == nil || errors.Is(err, ErrPromptNotDispatched) || !errors.Is(err, io.ErrUnexpectedEOF) || len(after.writes) != 2 {
		t.Fatalf("post-dispatch error = %v, writes = %q", err, after.writes)
	}
}

func TestTTYTransportRejectsUnboundedOrMalformedInputBeforeOpen(t *testing.T) {
	opened := 0
	transport := &TTYTransport{open: func(string) (serialSession, error) {
		opened++
		return nil, errors.New("must not open")
	}}
	if _, err := transport.Command(t.Context(), "/dev/ttyUSB2", "AT+CMGL=4\r", listTimeout); err == nil {
		t.Fatal("command containing CR was accepted")
	}
	if _, err := transport.Command(t.Context(), "/dev/ttyUSB2", "AT", sendTimeout+time.Second); err == nil {
		t.Fatal("unbounded timeout was accepted")
	}
	if _, err := transport.Prompt(t.Context(), "/dev/ttyUSB2", "AT+CMGS=1", []byte{'0', 'a', 0x1a}, sendTimeout); !errors.Is(err, ErrPromptNotDispatched) {
		t.Fatalf("malformed payload error = %v", err)
	}
	if opened != 0 {
		t.Fatalf("invalid exchange opened %d serial sessions", opened)
	}
}

func TestTTYTransportBoundsResponseAndHonorsCancellation(t *testing.T) {
	oversized := bytes.Repeat([]byte{'A'}, maxTTYResponseBytes+1)
	flood := &scriptedSerialSession{reads: []serialRead{{data: oversized}}}
	if _, err := fixtureTTYTransport(t, flood).Command(t.Context(), "/dev/ttyUSB2", "AT", modeTimeout); !errors.Is(err, ErrTTYResponseTooLarge) {
		t.Fatalf("oversized response error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocked := &scriptedSerialSession{reads: []serialRead{{waitContext: true}}}
	if _, err := fixtureTTYTransport(t, blocked).Command(ctx, "/dev/ttyUSB2", "AT", modeTimeout); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled response error = %v", err)
	}
}
