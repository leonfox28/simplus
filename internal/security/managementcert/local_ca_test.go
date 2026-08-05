package managementcert

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func TestGenerateLocalCACreatesNinetyDayLeafForNormalizedSANs(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	bundle, err := GenerateLocalCA(now, []string{"SIMPLUS.local.", "192.168.50.10", "192.168.50.10"})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.SANs) != 2 || bundle.SANs[0] != "simplus.local" || bundle.SANs[1] != "192.168.50.10" {
		t.Fatalf("SANs = %#v", bundle.SANs)
	}
	if bundle.LeafNotAfter.Sub(now) != 90*24*time.Hour {
		t.Fatalf("leaf lifetime = %s", bundle.LeafNotAfter.Sub(now))
	}
	if len(strings.Split(bundle.RootFingerprint, ":")) != 32 {
		t.Fatalf("root fingerprint = %q", bundle.RootFingerprint)
	}
	if _, err := tls.X509KeyPair(bundle.LeafCertificatePEM, bundle.LeafPrivateKeyPEM); err != nil {
		t.Fatal(err)
	}
	leafBlock, _ := pem.Decode(bundle.LeafCertificatePEM)
	caBlock, _ := pem.Decode(bundle.CACertificatePEM)
	if leafBlock == nil || caBlock == nil {
		t.Fatal("generated certificate PEM is invalid")
	}
	leaf, err := x509.ParseCertificate(leafBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	for _, name := range bundle.SANs {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: name, CurrentTime: now}); err != nil {
			t.Fatalf("verify %q: %v", name, err)
		}
	}
}

func TestGenerateLocalCARejectsUnsafeSANs(t *testing.T) {
	for _, sans := range [][]string{
		nil,
		{"*.example.com"},
		{"bad_name"},
		{strings.Repeat("a", 64) + ".example"},
	} {
		if _, err := GenerateLocalCA(time.Now(), sans); err == nil {
			t.Fatalf("accepted SANs %#v", sans)
		}
	}
}
