package vowifihil

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"
)

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
