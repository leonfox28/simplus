package managementcert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

type Bundle struct {
	CACertificatePEM   []byte
	CAPrivateKeyPEM    []byte
	LeafCertificatePEM []byte
	LeafPrivateKeyPEM  []byte
	RootFingerprint    string
	LeafNotAfter       time.Time
	SANs               []string
}

func GenerateLocalCA(now time.Time, subjectAlternativeNames []string) (Bundle, error) {
	normalizedSANs, dnsNames, ipAddresses, err := normalizeSANs(subjectAlternativeNames)
	if err != nil {
		return Bundle{}, err
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Bundle{}, fmt.Errorf("generate local CA key: %w", err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Bundle{}, fmt.Errorf("generate management leaf key: %w", err)
	}
	caSerial, err := randomSerial()
	if err != nil {
		return Bundle{}, err
	}
	leafSerial, err := randomSerial()
	if err != nil {
		return Bundle{}, err
	}
	now = now.UTC().Truncate(time.Second)
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "Simplus Local Root CA"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return Bundle{}, fmt.Errorf("create local CA certificate: %w", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: leafSerial,
		Subject:      pkix.Name{CommonName: normalizedSANs[0]},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(90 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		return Bundle{}, fmt.Errorf("create management leaf certificate: %w", err)
	}
	caKeyDER, err := x509.MarshalPKCS8PrivateKey(caKey)
	if err != nil {
		return Bundle{}, fmt.Errorf("encode local CA key: %w", err)
	}
	leafKeyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		return Bundle{}, fmt.Errorf("encode management leaf key: %w", err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: caKeyDER})
	leafKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: leafKeyDER})
	if _, err := tls.X509KeyPair(leafPEM, leafKeyPEM); err != nil {
		return Bundle{}, fmt.Errorf("self-check management keypair: %w", err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		return Bundle{}, fmt.Errorf("parse generated local CA: %w", err)
	}
	leafCertificate, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return Bundle{}, fmt.Errorf("parse generated management leaf: %w", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCertificate)
	for _, name := range normalizedSANs {
		if _, err := leafCertificate.Verify(x509.VerifyOptions{Roots: roots, DNSName: name, CurrentTime: now}); err != nil {
			return Bundle{}, fmt.Errorf("verify generated management certificate for %q: %w", name, err)
		}
	}
	fingerprintDigest := sha256.Sum256(caDER)
	return Bundle{
		CACertificatePEM:   caPEM,
		CAPrivateKeyPEM:    caKeyPEM,
		LeafCertificatePEM: leafPEM,
		LeafPrivateKeyPEM:  leafKeyPEM,
		RootFingerprint:    colonFingerprint(fingerprintDigest[:]),
		LeafNotAfter:       leafTemplate.NotAfter,
		SANs:               normalizedSANs,
	}, nil
}

func normalizeSANs(values []string) ([]string, []string, []net.IP, error) {
	if len(values) == 0 || len(values) > 16 {
		return nil, nil, nil, errors.New("management certificate requires between 1 and 16 SANs")
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	dnsNames := make([]string, 0, len(values))
	ipAddresses := make([]net.IP, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if ip := net.ParseIP(value); ip != nil {
			value = ip.String()
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			normalized = append(normalized, value)
			ipAddresses = append(ipAddresses, ip)
			continue
		}
		value = strings.TrimSuffix(strings.ToLower(value), ".")
		if !validDNSName(value) {
			return nil, nil, nil, fmt.Errorf("invalid management certificate SAN %q", raw)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
		dnsNames = append(dnsNames, value)
	}
	if len(normalized) == 0 {
		return nil, nil, nil, errors.New("management certificate SAN list became empty")
	}
	return normalized, dnsNames, ipAddresses, nil
}

func validDNSName(value string) bool {
	if value == "" || len(value) > 253 || strings.Contains(value, "*") {
		return false
	}
	labels := strings.Split(value, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	return serial, nil
}

func colonFingerprint(digest []byte) string {
	hexadecimal := strings.ToUpper(hex.EncodeToString(digest))
	parts := make([]string, 0, len(hexadecimal)/2)
	for index := 0; index < len(hexadecimal); index += 2 {
		parts = append(parts, hexadecimal[index:index+2])
	}
	return strings.Join(parts, ":")
}
