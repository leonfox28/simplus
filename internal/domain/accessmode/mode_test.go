package accessmode

import "testing"

func TestParse(t *testing.T) {
	for _, value := range []string{string(CellularNative), string(HostVoWiFiOnly), string(HoldRFOff)} {
		mode, err := Parse(value)
		if err != nil || string(mode) != value {
			t.Fatalf("Parse(%q) = %q, %v", value, mode, err)
		}
	}
	if _, err := Parse("automatic"); err == nil {
		t.Fatal("Parse accepted an unsupported access mode")
	}
}
