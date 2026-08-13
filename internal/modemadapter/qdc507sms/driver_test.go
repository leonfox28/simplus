package qdc507sms

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
	"github.com/leonfox28/simplus/internal/smscodec"
)

type transcriptStep struct {
	prompt  bool
	command string
	payload []byte
	timeout time.Duration
	lines   []string
	err     error
}

type transcriptTransport struct {
	t     *testing.T
	steps []transcriptStep
	next  int
}

func (transport *transcriptTransport) Command(_ context.Context, endpoint, command string, timeout time.Duration) ([]string, error) {
	transport.t.Helper()
	return transport.consume(false, endpoint, command, nil, timeout)
}

func (transport *transcriptTransport) Prompt(_ context.Context, endpoint, command string, payload []byte, timeout time.Duration) ([]string, error) {
	transport.t.Helper()
	return transport.consume(true, endpoint, command, payload, timeout)
}

func (transport *transcriptTransport) consume(prompt bool, endpoint, command string, payload []byte, timeout time.Duration) ([]string, error) {
	transport.t.Helper()
	if transport.next >= len(transport.steps) {
		transport.t.Fatalf("unexpected AT exchange prompt=%t endpoint=%q command=%q", prompt, endpoint, command)
	}
	step := transport.steps[transport.next]
	transport.next++
	if prompt != step.prompt || endpoint != "/dev/ttyUSB2" || command != step.command || timeout != step.timeout || !bytes.Equal(payload, step.payload) {
		transport.t.Fatalf("AT exchange = prompt=%t endpoint=%q command=%q payload=%x timeout=%s, want %#v", prompt, endpoint, command, payload, timeout, step)
	}
	return append([]string(nil), step.lines...), step.err
}

func (transport *transcriptTransport) assertDone() {
	transport.t.Helper()
	if transport.next != len(transport.steps) {
		transport.t.Fatalf("consumed %d of %d AT transcript steps", transport.next, len(transport.steps))
	}
}

func qdc507Device() agentapi.DeviceReport {
	return agentapi.DeviceReport{
		ID: "usb-1-1", Profile: agentapi.ProfileQDC507,
		Interfaces: []agentapi.USBInterface{{
			Number:    2,
			Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, InterfaceNumber: 2, Node: "/dev/ttyUSB2"}},
		}},
	}
}

func qdc507Target() modemadapter.SMSRuntimeTarget {
	return modemadapter.SMSRuntimeTarget{Device: qdc507Device(), SubscriptionKey: strings.Repeat("a", 64)}
}

func newTranscriptDriver(t *testing.T, steps ...transcriptStep) (*Driver, *transcriptTransport) {
	t.Helper()
	expanded := make([]transcriptStep, 0, len(steps)*2)
	for _, step := range steps {
		expanded = append(expanded, step)
		if step.command == "AT+CMGF=0" {
			expanded = append(expanded, transcriptStep{command: `AT+CPMS="SM","SM","SM"`, timeout: modeTimeout, lines: []string{"+CPMS: 1,20,1,20,1,20", "OK"}})
		}
	}
	transport := &transcriptTransport{t: t, steps: expanded}
	driver, err := NewDriver(transport)
	if err != nil {
		t.Fatal(err)
	}
	return driver, transport
}

func newExactTranscriptDriver(t *testing.T, steps ...transcriptStep) (*Driver, *transcriptTransport) {
	t.Helper()
	transport := &transcriptTransport{t: t, steps: append([]transcriptStep(nil), steps...)}
	driver, err := NewDriver(transport)
	if err != nil {
		t.Fatal(err)
	}
	return driver, transport
}

func modeStep() transcriptStep {
	return transcriptStep{command: "AT+CMGF=0", timeout: modeTimeout, lines: []string{"OK"}}
}

func TestDriverRequiresBoundedQuectelCPMSWriteResponse(t *testing.T) {
	for _, test := range []struct {
		name  string
		lines []string
	}{
		{name: "empty body", lines: []string{"OK"}},
		{name: "missing counter", lines: []string{"+CPMS: 1,20,1,20,1", "OK"}},
		{name: "used exceeds total", lines: []string{"+CPMS: 21,20,1,20,1,20", "OK"}},
		{name: "negative", lines: []string{"+CPMS: -1,20,1,20,1,20", "OK"}},
		{name: "overflow", lines: []string{"+CPMS: 1,65537,1,20,1,20", "OK"}},
		{name: "integer overflow", lines: []string{"+CPMS: 1,999999999999999999999,1,20,1,20", "OK"}},
		{name: "quoted read shape", lines: []string{`+CPMS: "SM",1,20,"SM",1,20,"SM",1,20`, "OK"}},
		{name: "terminal error", lines: []string{"+CMS ERROR: 302"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver, transport := newExactTranscriptDriver(t,
				modeStep(),
				transcriptStep{command: `AT+CPMS="SM","SM","SM"`, timeout: modeTimeout, lines: test.lines},
			)
			if _, err := driver.List(t.Context(), qdc507Device()); err == nil {
				t.Fatal("invalid CPMS response was accepted")
			}
			transport.assertDone()
		})
	}
}

func TestDriverAcceptsBoundedQuectelCPMSWriteResponse(t *testing.T) {
	for _, response := range []string{
		"+CPMS: 0,0,0,0,0,0",
		"+CPMS: 0,65536,1,20,20,20",
	} {
		t.Run(response, func(t *testing.T) {
			driver, transport := newExactTranscriptDriver(t,
				modeStep(),
				transcriptStep{command: `AT+CPMS="SM","SM","SM"`, timeout: modeTimeout, lines: []string{response, "OK"}},
				transcriptStep{command: "AT+CMGL=4", timeout: listTimeout, lines: []string{"OK"}},
			)
			if messages, err := driver.List(t.Context(), qdc507Device()); err != nil || len(messages) != 0 {
				t.Fatalf("messages=%#v error=%v", messages, err)
			}
			transport.assertDone()
		})
	}
}

func TestDriverReplaysBoundedListReadDeleteTranscript(t *testing.T) {
	const encodedPDU = "0891683108200105F0040D91685120012194F600F10180817144302304F4F29C0E"
	driver, transport := newTranscriptDriver(t,
		modeStep(),
		transcriptStep{command: "AT+CMGL=4", timeout: listTimeout, lines: []string{
			"+CMGL: 3,0,,24", encodedPDU, "OK",
		}},
		modeStep(),
		transcriptStep{command: "AT+CMGR=3", timeout: readTimeout, lines: []string{
			"+CMGR: 1,,24", encodedPDU, "OK",
		}},
		modeStep(),
		transcriptStep{command: "AT+CMGD=3,0", timeout: deleteTimeout, lines: []string{"OK"}},
	)

	listed, err := driver.List(context.Background(), qdc507Device())
	if err != nil || len(listed) != 1 || listed[0].Index != 3 || listed[0].Status != 0 || listed[0].TPDULength != 24 {
		t.Fatalf("list = %#v, error = %v", listed, err)
	}
	delivered, err := smscodec.DecodeDeliverPDU(listed[0].PDU)
	if err != nil || delivered.Sender != "+8615021012496" {
		t.Fatalf("decoded delivery = %#v, error = %v", delivered, err)
	}
	read, err := driver.Read(context.Background(), qdc507Device(), 3)
	if err != nil || read.Index != 3 || read.Status != 1 || !bytes.Equal(read.PDU, listed[0].PDU) {
		t.Fatalf("read = %#v, error = %v", read, err)
	}
	if err := driver.Delete(context.Background(), qdc507Device(), 3); err != nil {
		t.Fatal(err)
	}
	transport.assertDone()
}

func TestDriverReplaysExactGSM7SubmitTranscript(t *testing.T) {
	payload := append([]byte("0001000D91683108108300F0000005E8329BFD06"), 0x1a)
	driver, transport := newTranscriptDriver(t,
		modeStep(),
		transcriptStep{
			prompt: true, command: "AT+CMGS=19", payload: payload, timeout: sendTimeout,
			lines: []string{"+CMGS: 42", "OK"},
		},
	)
	result, err := driver.Send(context.Background(), qdc507Device(), "+8613800138000", "hello")
	if err != nil || len(result.Parts) != 1 || result.Parts[0].Part != 1 || result.Parts[0].Total != 1 || result.Parts[0].MessageReference != 42 {
		t.Fatalf("send = %#v, error = %v", result, err)
	}
	transport.assertDone()
}

func TestDriverReportsPartialMultipartSubmissionWithoutBlindRetry(t *testing.T) {
	text := strings.Repeat("a", 161)
	segments, err := smscodec.Encode(text)
	if err != nil {
		t.Fatal(err)
	}
	steps := []transcriptStep{modeStep()}
	for index, segment := range segments {
		pdu, err := smscodec.EncodeSubmitPDU("10086", segment, byte(segment.Reference)+byte(index))
		if err != nil {
			t.Fatal(err)
		}
		lines := []string{"+CMGS: " + strconv.Itoa(70+index), "OK"}
		if index == 1 {
			lines = []string{"+CMS ERROR: 331"}
		}
		steps = append(steps, transcriptStep{
			prompt: true, command: "AT+CMGS=" + strconv.Itoa(pdu.TPDULength),
			payload: append([]byte(pdu.Hex()), 0x1a), timeout: sendTimeout, lines: lines,
		})
	}
	driver, transport := newTranscriptDriver(t, steps...)
	result, err := driver.Send(context.Background(), qdc507Device(), "10086", text)
	if !errors.Is(err, ErrModemRejected) || len(result.Parts) != 1 {
		t.Fatalf("partial send = %#v, error = %v", result, err)
	}
	var failure *SendFailure
	var modemError *ModemError
	if !errors.As(err, &failure) || failure.CompletedParts != 1 || failure.TotalParts != 2 ||
		!errors.As(err, &modemError) || modemError.CMSCode != 331 || modemError.Uncertain {
		t.Fatalf("partial failure = %#v, modem error = %#v", failure, modemError)
	}
	transport.assertDone()
}

func TestDriverMarksLostPostDispatchResponseUncertain(t *testing.T) {
	driver, transport := newTranscriptDriver(t,
		modeStep(),
		transcriptStep{
			prompt: true, command: "AT+CMGS=19",
			payload: append([]byte("0001000D91683108108300F0000005E8329BFD06"), 0x1a), timeout: sendTimeout,
			err: io.ErrUnexpectedEOF,
		},
	)
	_, err := driver.Send(context.Background(), qdc507Device(), "+8613800138000", "hello")
	if !errors.Is(err, ErrSendOutcomeUnknown) {
		t.Fatalf("send error = %v", err)
	}
	var modemError *ModemError
	if !errors.As(err, &modemError) || !modemError.Uncertain || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("uncertain error = %#v", modemError)
	}
	transport.assertDone()
}

func TestDriverKeepsPromptFailureBeforePayloadRetryable(t *testing.T) {
	driver, transport := newTranscriptDriver(t,
		modeStep(),
		transcriptStep{
			prompt: true, command: "AT+CMGS=19",
			payload: append([]byte("0001000D91683108108300F0000005E8329BFD06"), 0x1a), timeout: sendTimeout,
			err: errors.Join(ErrPromptNotDispatched, io.ErrUnexpectedEOF),
		},
	)
	_, err := driver.Send(context.Background(), qdc507Device(), "+8613800138000", "hello")
	if !errors.Is(err, ErrTransport) || errors.Is(err, ErrSendOutcomeUnknown) || !errors.Is(err, ErrPromptNotDispatched) {
		t.Fatalf("pre-dispatch send error = %v", err)
	}
	transport.assertDone()
}

func TestDriverMarksMalformedPostDispatchResultUncertain(t *testing.T) {
	driver, transport := newTranscriptDriver(t,
		modeStep(),
		transcriptStep{
			prompt: true, command: "AT+CMGS=19",
			payload: append([]byte("0001000D91683108108300F0000005E8329BFD06"), 0x1a), timeout: sendTimeout,
			lines: []string{"OK"},
		},
	)
	_, err := driver.Send(context.Background(), qdc507Device(), "+8613800138000", "hello")
	if !errors.Is(err, ErrSendOutcomeUnknown) {
		t.Fatalf("send error = %v", err)
	}
	var modemError *ModemError
	if !errors.As(err, &modemError) || !modemError.Uncertain {
		t.Fatalf("uncertain error = %#v", modemError)
	}
	transport.assertDone()
}

func TestDriverMapsInvalidMemoryIndexToMessageNotFound(t *testing.T) {
	driver, transport := newTranscriptDriver(t,
		modeStep(),
		transcriptStep{command: "AT+CMGR=99", timeout: readTimeout, lines: []string{"+CMS ERROR: 321"}},
	)
	_, err := driver.Read(context.Background(), qdc507Device(), 99)
	if !errors.Is(err, agentapi.ErrSMSMessageNotFound) || !errors.Is(err, ErrModemRejected) {
		t.Fatalf("read error = %v", err)
	}
	transport.assertDone()
}

func TestDriverRejectsMalformedOrUnroutableTranscripts(t *testing.T) {
	driver, transport := newTranscriptDriver(t,
		modeStep(),
		transcriptStep{command: "AT+CMGL=4", timeout: listTimeout, lines: []string{
			"+CMGL: 3,0,,20", "0001000D91683108108300F0000005E8329BFD06", "OK",
		}},
	)
	if _, err := driver.List(context.Background(), qdc507Device()); !errors.Is(err, ErrResponseInvalid) {
		t.Fatalf("malformed list error = %v", err)
	}
	transport.assertDone()

	missing := qdc507Device()
	missing.Interfaces = nil
	if _, err := driver.Send(context.Background(), missing, "10086", "hello"); !errors.Is(err, ErrControlEndpoint) {
		t.Fatalf("missing endpoint error = %v", err)
	}
}

func TestCandidateDriverCannotEnableAgentSMSRoutes(t *testing.T) {
	driver, _ := newTranscriptDriver(t)
	if _, registered := any(driver).(modemadapter.SMSAdapter); registered {
		t.Fatal("candidate QDC507 driver unexpectedly implements the complete Agent SMS adapter")
	}
	if modemadapter.DefaultRegistry().SupportsSMS() {
		t.Fatal("candidate QDC507 driver unexpectedly changed the default Agent registry")
	}
}

func TestSendTimeoutBudgetsCoverTheWholeAgentRequest(t *testing.T) {
	if sendTimeout != agentapi.SMSDispatchTimeout {
		t.Fatalf("driver send timeout = %s, Agent dispatch timeout = %s", sendTimeout, agentapi.SMSDispatchTimeout)
	}
	if agentapi.SMSRequestTimeout <= sendTimeout {
		t.Fatalf("Agent SMS request timeout %s does not cover modem timeout %s", agentapi.SMSRequestTimeout, sendTimeout)
	}
}
