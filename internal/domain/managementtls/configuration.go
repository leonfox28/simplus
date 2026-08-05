package managementtls

import "time"

type Mode string

const (
	ModeLoopbackOnly Mode = "loopback-only"
	ModeLocalCA      Mode = "local-ca"
	ModeImported     Mode = "imported"
)

func (mode Mode) Valid() bool {
	return mode == ModeLoopbackOnly || mode == ModeLocalCA || mode == ModeImported
}

type Configuration struct {
	Mode                    Mode
	ListenHost              string
	ListenPort              int
	SubjectAlternativeNames []string
	CACertificatePEM        []byte
	LeafCertificatePEM      []byte
	EncryptedCAPrivateKey   []byte
	EncryptedLeafPrivateKey []byte
	RootFingerprintSHA256   string
	LeafNotAfter            time.Time
	Confirmed               bool
	ConfiguredAt            time.Time
}
