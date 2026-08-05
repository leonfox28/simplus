package mihomo

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseSubscriptionNodesKeepsOnlyProxyEntriesAndClassifiesCountry(t *testing.T) {
	body := []byte("proxies:\n  - name: Tokyo A\n    type: ss\n    server: secret.example\n    password: do-not-store\n  - name: US B\n    type: vless\n    uuid: secret\n")
	nodes, err := parseSubscriptionNodes("subscription_abcdefghijklmnopqrstuv", body)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].DisplayName != "Tokyo A" || nodes[1].DisplayName != "US B" {
		t.Fatalf("nodes = %#v", nodes)
	}
	for _, node := range nodes {
		if node.Kind != "ss" && node.Kind != "vless" {
			t.Fatalf("node = %#v", node)
		}
		if node.ProxyYAML == "" || strings.Contains(node.ProxyYAML, "listeners:") {
			t.Fatalf("node config = %#v", node)
		}
	}
	if nodes[1].CountryCode != "US" || nodes[1].CountryName != "美国" {
		t.Fatalf("country = %#v", nodes[1])
	}
}

func TestParseSubscriptionNodesSupportsBase64URIListAndRejectsEmpty(t *testing.T) {
	raw := "trojan://secret@example.com:443#Tokyo%20Trojan\nss://secret@example.com:443#US%20SS\n"
	nodes, err := parseSubscriptionNodes("subscription_abcdefghijklmnopqrstuv", []byte(base64.StdEncoding.EncodeToString([]byte(raw))))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].DisplayName != "Tokyo Trojan" || nodes[1].DisplayName != "US SS" {
		t.Fatalf("nodes = %#v", nodes)
	}
	if _, err := parseSubscriptionNodes("subscription_abcdefghijklmnopqrstuv", []byte("not a subscription")); err == nil {
		t.Fatal("invalid subscription accepted")
	}
}

func TestValidateSubscriptionInputRejectsPrivateTargets(t *testing.T) {
	for _, raw := range []string{"http://example.com/sub", "https://localhost/sub", "https://127.0.0.1/sub", "https://192.168.50.1/sub", "https://[::1]/sub"} {
		if _, _, err := validateSubscriptionInput("test", raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if _, parsed, err := validateSubscriptionInput("test", "https://subscription.example/path?token=secret"); err != nil || parsed.Hostname() != "subscription.example" {
		t.Fatalf("public URL = %v, %v", parsed, err)
	}
}

func TestSubscriptionIdentifiersSeparateStableIdentityFromDefaultDisplayName(t *testing.T) {
	id, err := newSubscriptionID()
	if err != nil {
		t.Fatal(err)
	}
	if !subscriptionIDPattern.MatchString(id) {
		t.Fatalf("id=%q", id)
	}
	name := defaultSubscriptionDisplayName(id)
	if !subscriptionDefaultNamePattern.MatchString(name) || name != defaultSubscriptionDisplayName(id) {
		t.Fatalf("default display name=%q", name)
	}
}
