package accessmode

import "fmt"

type Mode string

const (
	CellularNative Mode = "cellular-native"
	HostVoWiFiOnly Mode = "host-vowifi-only"
	HoldRFOff      Mode = "hold-rf-off"
)

func Parse(value string) (Mode, error) {
	mode := Mode(value)
	if !mode.Valid() {
		return "", fmt.Errorf("unsupported access mode %q", value)
	}
	return mode, nil
}

func (mode Mode) Valid() bool {
	switch mode {
	case CellularNative, HostVoWiFiOnly, HoldRFOff:
		return true
	default:
		return false
	}
}
