package vowifihil

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestIMSRegistrationFailureCodeIsBoundedAndPreservesCause(t *testing.T) {
	cause := errors.New("private protocol detail")
	err := registrationFailure("IMS_PROTECTED_NO_RESPONSE", cause)
	if got := IMSRegistrationFailureCode(err); got != "IMS_PROTECTED_NO_RESPONSE" {
		t.Fatalf("failure code = %q", got)
	}
	if !errors.Is(err, cause) {
		t.Fatal("registration failure does not preserve its internal cause")
	}
	if got := IMSRegistrationFailureCode(cause); got != "" {
		t.Fatalf("ordinary error exposed code %q", got)
	}
	if got := IMSRegistrationFailureCode(registrationFailure("PRIVATE_DETAIL", cause)); got != "" {
		t.Fatalf("unapproved registration code escaped as %q", got)
	}
}

func TestIMSRegistrationExchangeCodesRemainCredentialFree(t *testing.T) {
	for err, want := range map[error]string{
		errIMSRegisterNoResponse:    "IMS_INITIAL_NO_RESPONSE",
		errIMSRegisterSend:          "IMS_INITIAL_SEND_FAILED",
		errIMSRegisterRead:          "IMS_INITIAL_READ_FAILED",
		errProtectedIMSNoResponse:   "IMS_PROTECTED_NO_RESPONSE",
		errProtectedIMSUnmatched:    "IMS_PROTECTED_RESPONSE_UNMATCHED",
		errProtectedIMSRegisterSend: "IMS_PROTECTED_SEND_FAILED",
		errProtectedIMSRegisterRead: "IMS_PROTECTED_READ_FAILED",
	} {
		got := initialRegistrationExchangeCode(err)
		if strings.HasPrefix(want, "IMS_PROTECTED_") {
			got = protectedRegistrationExchangeCode(err)
		}
		if got != want {
			t.Fatalf("exchange code for %v = %q, want %q", err, got, want)
		}
	}
}

func TestRegistrationExpiresSecondsPreservesAcceptedInterval(t *testing.T) {
	value, ok := registrationExpiresSeconds(30 * time.Minute)
	if !ok || value != 1800 {
		t.Fatalf("registration interval = %d, %v; want 1800, true", value, ok)
	}
	for _, invalid := range []time.Duration{59 * time.Second, 24*time.Hour + time.Second} {
		if value, ok := registrationExpiresSeconds(invalid); ok || value != 0 {
			t.Fatalf("invalid registration interval %s = %d, %v", invalid, value, ok)
		}
	}
}

func TestRefreshDigestStateReusesClientNonce(t *testing.T) {
	session := &IMSSession{
		challenge:  IMSRegistrationChallenge{QOP: "auth"},
		cnonce:     "0123456789abcdef",
		nonceCount: 1,
	}
	cnonce, nonceCount, err := session.nextRefreshDigestState()
	if err != nil || cnonce != session.cnonce || nonceCount != 2 {
		t.Fatalf("refresh digest = %q, %d, %v", cnonce, nonceCount, err)
	}
	session.cnonce = ""
	if _, _, err := session.nextRefreshDigestState(); err == nil {
		t.Fatal("refresh accepted a missing qop client nonce")
	}
}

func TestKeepaliveClearsSocketWriteDeadline(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	receiverPort := receiver.LocalAddr().(*net.UDPAddr).Port
	session := &IMSSession{
		client: client,
		pcscf:  netip.MustParseAddr("127.0.0.1"),
		challenge: IMSRegistrationChallenge{SecurityServer: IMSIPSecParameters{
			ProtectedServerPort: uint16(receiverPort),
		}},
	}
	deadline := time.Now().Add(50 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if err := session.Keepalive(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(deadline) + 20*time.Millisecond)
	if _, err := client.WriteToUDP([]byte("probe"), receiver.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatalf("write after keepalive deadline: %v", err)
	}
}
