package attransport

import (
	"strings"
	"testing"
)

func TestTerminalResponseClassification(t *testing.T) {
	if !HasTerminalOK([]string{"+VALUE: 1", "OK"}) || !HasTerminalResponse([]string{"+CME ERROR: 10"}) {
		t.Fatal("terminal AT responses were not recognized")
	}
	if HasTerminalOK([]string{"ERROR"}) || HasTerminalResponse([]string{"+VALUE: 1"}) {
		t.Fatal("non-success or incomplete responses were accepted")
	}
}

func TestSplitLinesRemovesEchoAndBoundsText(t *testing.T) {
	lines := splitLines("AT+TEST\r\n+VALUE: 1\r\nOK\r\n", "AT+TEST")
	if len(lines) != 2 || lines[0] != "+VALUE: 1" || lines[1] != "OK" {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestSplitLinesPreservesBoundedAdapterPayloads(t *testing.T) {
	payload := strings.Repeat("A", 512)
	lines := splitLines("+VALUE: "+payload+"\r\nOK\r\n", "query")
	if len(lines) != 2 || lines[0] != "+VALUE: "+payload || lines[1] != "OK" {
		t.Fatalf("lines = %#v", lines)
	}
}
