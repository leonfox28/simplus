package main

import "testing"

func TestPrivateIPv4Fence(t *testing.T) {
	for _, valid := range []string{"10.1.2.3", "172.16.0.1", "192.168.4.5"} {
		if _, err := privateIPv4(valid); err != nil {
			t.Fatalf("privateIPv4(%q): %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "1.1.1.1", "127.0.0.1", "2001:db8::1"} {
		if _, err := privateIPv4(invalid); err == nil {
			t.Fatalf("privateIPv4(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestParseSIPStatus(t *testing.T) {
	if status, ok := parseSIPStatus([]byte("SIP/2.0 403 Forbidden\r\nServer: hidden\r\n\r\n")); !ok || status != 403 {
		t.Fatalf("status=%d ok=%t", status, ok)
	}
	for _, invalid := range [][]byte{
		[]byte("HTTP/1.1 200 OK\r\n\r\n"),
		[]byte("SIP/2.0 invalid\r\n\r\n"),
		[]byte("SIP/2.0 999 invalid\r\n\r\n"),
	} {
		if status, ok := parseSIPStatus(invalid); ok {
			t.Fatalf("invalid response status=%d", status)
		}
	}
}
