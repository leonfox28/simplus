package hardwareprobe

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type atQueryStep struct {
	command  string
	response []string
	err      error
}

func scriptedATQuery(t *testing.T, steps []atQueryStep) (boundedATQuery, *[]string) {
	t.Helper()
	commands := make([]string, 0, len(steps))
	index := 0
	query := func(_ context.Context, command string, timeout time.Duration) ([]string, error) {
		t.Helper()
		if timeout <= 0 {
			t.Fatalf("nonpositive timeout for %q", command)
		}
		commands = append(commands, command)
		if index >= len(steps) {
			t.Fatalf("unexpected command %q", command)
		}
		step := steps[index]
		index++
		if command != step.command {
			t.Fatalf("command %d = %q, want %q", index, command, step.command)
		}
		return step.response, step.err
	}
	return query, &commands
}

func TestEnsureRadioOffSkipsWriteWhenAlreadyOff(t *testing.T) {
	query, commands := scriptedATQuery(t, []atQueryStep{
		{command: "AT", response: []string{"OK"}},
		{command: "AT+CLCC", response: []string{"OK"}},
		{command: "AT+CFUN?", response: []string{"+CFUN: 4", "OK"}},
	})
	execution := ensureRadioOffWithQuery(context.Background(), query)
	if execution.Error != nil || execution.Dispatched || execution.Observation.RF.State != agentapi.RFStateOff ||
		execution.Observation.ActiveCallCount == nil || *execution.Observation.ActiveCallCount != 0 {
		t.Fatalf("execution = %#v", execution)
	}
	if !reflect.DeepEqual(*commands, []string{"AT", "AT+CLCC", "AT+CFUN?"}) {
		t.Fatalf("commands = %#v", *commands)
	}
}

func TestEnsureRadioOffRejectsActiveCallBeforeRFQueryOrWrite(t *testing.T) {
	query, commands := scriptedATQuery(t, []atQueryStep{
		{command: "AT", response: []string{"OK"}},
		{command: "AT+CLCC", response: []string{`+CLCC: 1,0,0,0,0,"+441234567890",145`, "OK"}},
	})
	execution := ensureRadioOffWithQuery(context.Background(), query)
	if execution.Error == nil || execution.Error.Code != agentapi.ErrorActiveCallPresent || execution.Dispatched {
		t.Fatalf("execution = %#v", execution)
	}
	if !reflect.DeepEqual(*commands, []string{"AT", "AT+CLCC"}) {
		t.Fatalf("commands = %#v", *commands)
	}
}

func TestEnsureRadioOffDispatchesOnlyFixedCommandAndConfirmsState(t *testing.T) {
	query, commands := scriptedATQuery(t, []atQueryStep{
		{command: "AT", response: []string{"OK"}},
		{command: "AT+CLCC", response: []string{"OK"}},
		{command: "AT+CFUN?", response: []string{"+CFUN: 2", "OK"}},
		{command: "AT+CFUN=4", response: []string{"OK"}},
		{command: "AT+CFUN?", response: []string{"+CFUN: 4", "OK"}},
	})
	execution := ensureRadioOffWithQuery(context.Background(), query)
	if execution.Error != nil || !execution.Dispatched || execution.Uncertain || execution.Observation.RF.State != agentapi.RFStateOff {
		t.Fatalf("execution = %#v", execution)
	}
	want := []string{"AT", "AT+CLCC", "AT+CFUN?", "AT+CFUN=4", "AT+CFUN?"}
	if !reflect.DeepEqual(*commands, want) {
		t.Fatalf("commands = %#v, want %#v", *commands, want)
	}
}

func TestEnsureRadioOffMarksLostDispatchResponseUncertain(t *testing.T) {
	query, _ := scriptedATQuery(t, []atQueryStep{
		{command: "AT", response: []string{"OK"}},
		{command: "AT+CLCC", response: []string{"OK"}},
		{command: "AT+CFUN?", response: []string{"+CFUN: 1", "OK"}},
		{command: "AT+CFUN=4", err: errors.New("timeout")},
	})
	execution := ensureRadioOffWithQuery(context.Background(), query)
	if execution.Error == nil || execution.Error.Code != agentapi.ErrorRadioOffOutcomeUncertain || !execution.Dispatched || !execution.Uncertain {
		t.Fatalf("execution = %#v", execution)
	}
}

func TestEnsureRadioOffRequiresPostCommandOffObservation(t *testing.T) {
	query, _ := scriptedATQuery(t, []atQueryStep{
		{command: "AT", response: []string{"OK"}},
		{command: "AT+CLCC", response: []string{"OK"}},
		{command: "AT+CFUN?", response: []string{"+CFUN: 1", "OK"}},
		{command: "AT+CFUN=4", response: []string{"OK"}},
		{command: "AT+CFUN?", response: []string{"+CFUN: 1", "OK"}},
	})
	execution := ensureRadioOffWithQuery(context.Background(), query)
	if execution.Error == nil || execution.Error.Code != agentapi.ErrorRadioOffNotConfirmed || !execution.Dispatched || execution.Uncertain {
		t.Fatalf("execution = %#v", execution)
	}
}
