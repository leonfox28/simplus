package vowifihil

import (
	"net/netip"
	"strings"
	"testing"
)

func TestParseEPDGTunnelPolicy(t *testing.T) {
	output := `src 192.0.2.42/32 dst 0.0.0.0/0
	dir out priority 371327 ptype main
	tmpl src 169.254.248.2 dst 88.82.11.221
		proto esp spi 0x019a83df reqid 1 mode tunnel
src 169.254.248.2/32 dst 88.82.11.221/32 proto udp sport 4500 dport 4500
	dir out priority 100 ptype main
`
	parsed, err := parseEPDGTunnelPolicy(output, netip.MustParseAddr("192.0.2.42"))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Local.String() != "169.254.248.2" || parsed.Remote.String() != "88.82.11.221" || parsed.ReqID != 1 {
		t.Fatalf("unexpected template %#v", parsed)
	}
}

func TestParseEPDGTunnelPolicyRejectsAmbiguousOrForeignGateway(t *testing.T) {
	base := `src 192.0.2.42/32 dst 0.0.0.0/0
	dir out priority 371327 ptype main
	tmpl src 169.254.248.2 dst %s
		proto esp spi 0x019a83df reqid 1 mode tunnel
`
	if _, err := parseEPDGTunnelPolicy(strings.ReplaceAll(base, "%s", "203.0.113.8"),
		netip.MustParseAddr("192.0.2.42")); err == nil {
		t.Fatal("accepted a foreign ePDG gateway")
	}
	double := strings.ReplaceAll(base, "%s", "88.82.11.221") + strings.ReplaceAll(base, "%s", "88.82.11.208")
	if _, err := parseEPDGTunnelPolicy(double, netip.MustParseAddr("192.0.2.42")); err == nil {
		t.Fatal("accepted ambiguous ePDG policies")
	}
}

func TestBuildIMSXFRMCleanupContainsOnlySelectorsAndSPIs(t *testing.T) {
	client := IMSClientIPSecParameters{
		ClientSPI: 1234567890, ServerSPI: 2234567890,
		ProtectedClientPort: 42001, ProtectedServerPort: 42002,
	}
	server := IMSIPSecParameters{
		ClientSPI: 3234567890, ServerSPI: 4234567890,
		ProtectedClientPort: 43001, ProtectedServerPort: 43002,
	}
	cleanup := buildIMSXFRMCleanup(netip.MustParseAddr("192.0.2.42"),
		netip.MustParseAddr("198.51.100.53"), client, server)
	text := string(cleanup)
	if strings.Count(text, "xfrm policy delete") != 4 || strings.Count(text, "xfrm state delete") != 4 ||
		strings.Contains(text, "auth") || strings.Contains(text, "enc ") {
		t.Fatalf("unexpected cleanup batch %q", text)
	}
}
