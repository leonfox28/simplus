package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHardwareWriteFlagsAreNotAccepted(t *testing.T) {
	for _, name := range []string{"--enable-radio-ensure-off", "--enable-qdc507-sms"} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if status := run([]string{name}, &stdout, &stderr); status != 2 {
			t.Fatalf("run(%q) status = %d, want 2", name, status)
		}
		if !strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("run(%q) stderr = %q", name, stderr.String())
		}
	}
}
