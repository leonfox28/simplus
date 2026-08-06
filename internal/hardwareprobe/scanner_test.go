package hardwareprobe

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
)

type fakeQuerier struct {
	endpoint               string
	profile                string
	equipmentFingerprint   string
	probeState             string
	sim                    agentapi.SIMObservation
	simIdentityFingerprint string
	simIdentityHint        string
}

func (querier *fakeQuerier) Probe(_ context.Context, endpoint string, adapter modemadapter.Adapter) agentapi.DeviceProbe {
	querier.endpoint = endpoint
	querier.profile = adapter.Profile()
	count := 0
	state := querier.probeState
	if state == "" {
		state = agentapi.ProbeStateComplete
	}
	sim := querier.sim
	if sim.State == "" {
		sim = agentapi.SIMObservation{State: agentapi.SIMStateAbsent, PrimaryLockState: agentapi.PrimaryLockUnknown}
	}
	sim.IdentityFingerprint = querier.simIdentityFingerprint
	sim.DisplayIdentityHint = querier.simIdentityHint
	return agentapi.DeviceProbe{
		State:           state,
		RF:              agentapi.RFObservation{State: agentapi.RFStateOff},
		SIM:             sim,
		SignalMetrics:   agentapi.SignalObservation{State: agentapi.SignalStateUnknown},
		Registrations:   []agentapi.RegistrationObservation{},
		CurrentNetwork:  agentapi.NetworkObservation{SelectionMode: agentapi.NetworkSelectionUnknown},
		ActiveCallCount: &count,
		Identity:        agentapi.ModemIdentity{EquipmentIdentityFingerprint: querier.equipmentFingerprint},
	}
}

type deterministicPseudonymizer struct{}

func (deterministicPseudonymizer) Pseudonym(label string, value []byte) (string, error) {
	digest := sha256.Sum256(append(append([]byte(label), 0), value...))
	return fmt.Sprintf("%x", digest[:]), nil
}

func TestStableDeviceIDNormalizesHubPortSeparators(t *testing.T) {
	if got := stableDeviceID("1-2.3.4"); got != "usb-1-2-port-3-port-4" {
		t.Fatalf("stable id = %q", got)
	}
}

func TestScannerDiscoversKnownDevicesWithoutExposingUSBSerial(t *testing.T) {
	root := t.TempDir()
	usbRoot := filepath.Join(root, "sys", "bus", "usb", "devices")
	devRoot := filepath.Join(root, "dev")
	mustMkdir(t, usbRoot)
	mustMkdir(t, devRoot)
	writeUSBDevice(t, usbRoot, "1-1", map[string]string{
		"idVendor": "2c7c", "idProduct": "0125", "manufacturer": "BAIWANG", "product": "Baiwang",
		"serial": "must-not-leave-sysfs", "bcdDevice": "0318", "bConfigurationValue": "1",
	})
	writeInterface(t, usbRoot, "1-1:1.2", 2, "ff", "00", "00", "option", "ttyUSB2")
	writeInterface(t, usbRoot, "1-1:1.4", 4, "ff", "ff", "ff", "qmi_wwan", "")
	mustMkdir(t, filepath.Join(usbRoot, "1-1:1.4", "usbmisc", "cdc-wdm0"))
	mustMkdir(t, filepath.Join(usbRoot, "1-1:1.4", "net", "wwan0"))
	writeInterface(t, usbRoot, "1-1:1.5", 5, "01", "01", "00", "snd-usb-audio", "")
	mustMkdir(t, filepath.Join(usbRoot, "1-1:1.5", "sound", "card0"))
	writeUSBDevice(t, usbRoot, "1-3", map[string]string{
		"idVendor": "2ecc", "idProduct": "3012", "manufacturer": "CMIOT", "product": "ML307A",
		"serial": "another-secret", "bcdDevice": "0100", "bConfigurationValue": "1",
	})
	writeInterface(t, usbRoot, "1-3:1.2", 2, "ff", "00", "00", "option", "ttyUSB6")
	writeUSBDevice(t, usbRoot, "2-1", map[string]string{"idVendor": "1234", "idProduct": "5678", "product": "ignored"})

	querier := &fakeQuerier{equipmentFingerprint: strings.Repeat("e", 64)}
	scanner := &Scanner{USBRoot: usbRoot, DevRoot: devRoot, Querier: querier, Identities: deterministicPseudonymizer{}}
	devices, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %#v", devices)
	}
	qdc := devices[0]
	if qdc.ID != "usb-1-1" || qdc.Profile != agentapi.ProfileQDC507 || !qdc.USB.SerialPresent {
		t.Fatalf("qdc = %#v", qdc)
	}
	if !fingerprintPattern.MatchString(qdc.USB.SerialFingerprint) || !fingerprintPattern.MatchString(devices[1].USB.SerialFingerprint) ||
		qdc.USB.SerialFingerprint == devices[1].USB.SerialFingerprint {
		t.Fatalf("USB serial fingerprints were not stable, distinct pseudonyms: %#v", devices)
	}
	encoded, err := json.Marshal(devices)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "must-not-leave-sysfs") || strings.Contains(string(encoded), "another-secret") {
		t.Fatalf("raw serial leaked: %s", encoded)
	}
	if !hasCapability(qdc, "usb-uac", agentapi.EvidenceObserved) || !hasCapability(qdc, "digital-voice-media", agentapi.EvidenceUnverified) {
		t.Fatalf("qdc capabilities = %#v", qdc.Capabilities)
	}
	if !hasCapability(devices[1], "at-control", agentapi.EvidenceObserved) ||
		!hasCapability(devices[1], "sim-apdu", agentapi.EvidenceObserved) ||
		!hasCapability(devices[1], "digital-voice-media", agentapi.EvidenceUnsupported) {
		t.Fatalf("ML307A evidence = %#v", devices[1].Capabilities)
	}

	snapshot := agentapi.Snapshot{Generation: 3, Devices: devices}
	probes, err := scanner.Probe(context.Background(), snapshot, []string{qdc.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 || probes[0].State != agentapi.ProbeStateComplete {
		t.Fatalf("probes = %#v", probes)
	}
	if querier.endpoint != filepath.Join(devRoot, "ttyUSB2") {
		t.Fatalf("query endpoint = %q", querier.endpoint)
	}
	if querier.profile != agentapi.ProfileQDC507 {
		t.Fatalf("QDC507 adapter profile = %q", querier.profile)
	}
	if _, err := scanner.Probe(context.Background(), snapshot, []string{"missing"}); err == nil {
		t.Fatal("missing device probe unexpectedly succeeded")
	}
	ml307aProbes, err := scanner.Probe(context.Background(), snapshot, []string{devices[1].ID})
	if err != nil || len(ml307aProbes) != 1 || ml307aProbes[0].Identity.EquipmentIdentityFingerprint != querier.equipmentFingerprint ||
		querier.profile != agentapi.ProfileML307A {
		t.Fatalf("ML307A adapter routing probes=%#v profile=%q error=%v", ml307aProbes, querier.profile, err)
	}
}

func TestScannerReportsMissingPreferredATEndpointAsRetryableDeviceError(t *testing.T) {
	scanner := &Scanner{USBRoot: "/unused", DevRoot: "/dev", Querier: &fakeQuerier{}}
	snapshot := agentapi.Snapshot{Generation: 7, Devices: []agentapi.DeviceReport{{
		ID: "usb-1-1", Profile: agentapi.ProfileQDC507,
	}}}
	probes, err := scanner.Probe(context.Background(), snapshot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 {
		t.Fatalf("probes = %#v", probes)
	}
	probe := probes[0]
	if probe.State != agentapi.ProbeStateUnavailable || probe.Error == nil ||
		probe.Error.Layer != agentapi.ErrorLayerDevice || probe.Error.Code != agentapi.ErrorControlEndpointMissing || !probe.Error.Retryable {
		t.Fatalf("missing endpoint probe = %#v", probe)
	}
	if probe.SIM.PrimaryLockState != agentapi.PrimaryLockUnknown || probe.Registrations == nil {
		t.Fatalf("typed defaults = %#v", probe)
	}
	encoded, err := json.Marshal(probe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"registrations":[]`) {
		t.Fatalf("registrations did not serialize as an array: %s", encoded)
	}
}

func TestScannerReportsMissingML307AEndpointWithTypedDefaults(t *testing.T) {
	scanner := &Scanner{USBRoot: "/unused", DevRoot: "/dev", Querier: &fakeQuerier{}}
	snapshot := agentapi.Snapshot{Generation: 3, Devices: []agentapi.DeviceReport{{
		ID: "usb-1-3", Profile: agentapi.ProfileML307A,
	}}}
	probes, err := scanner.Probe(context.Background(), snapshot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 || probes[0].State != agentapi.ProbeStateUnavailable || probes[0].ErrorCode != agentapi.ErrorControlEndpointMissing ||
		probes[0].SIM.PrimaryLockState != agentapi.PrimaryLockUnknown || probes[0].Registrations == nil {
		t.Fatalf("missing-endpoint probe = %#v", probes)
	}
}

func TestScannerReadsStableIdentitiesIndependentlyFromCompositeProbeState(t *testing.T) {
	equipmentFingerprint := strings.Repeat("e", 64)
	simFingerprint := strings.Repeat("f", 64)
	querier := &fakeQuerier{
		probeState: agentapi.ProbeStateFailed, equipmentFingerprint: equipmentFingerprint,
		sim:                    agentapi.SIMObservation{State: agentapi.SIMStatePresent, PrimaryLockState: agentapi.PrimaryLockReady},
		simIdentityFingerprint: simFingerprint, simIdentityHint: "ICCID •••• 1234",
	}
	scanner := &Scanner{USBRoot: "/unused", DevRoot: "/dev", Querier: querier}
	snapshot := agentapi.Snapshot{Generation: 4, Devices: []agentapi.DeviceReport{{
		ID: "usb-1-3", Profile: agentapi.ProfileML307A,
		Interfaces: []agentapi.USBInterface{{Number: 2, Endpoints: []agentapi.Endpoint{{
			Kind: agentapi.EndpointTTY, InterfaceNumber: 2, Node: "/dev/fixture-ml307a",
		}}}},
	}}}
	probes, err := scanner.Probe(t.Context(), snapshot, nil)
	if err != nil || len(probes) != 1 {
		t.Fatalf("probes=%#v error=%v", probes, err)
	}
	probe := probes[0]
	if probe.State != agentapi.ProbeStateFailed || probe.Identity.EquipmentIdentityFingerprint != equipmentFingerprint ||
		probe.SIM.IdentityFingerprint != simFingerprint || probe.SIM.DisplayIdentityHint != "ICCID •••• 1234" {
		t.Fatalf("independent identity observations = %#v", probe)
	}
	if querier.profile != agentapi.ProfileML307A {
		t.Fatalf("adapter profile = %q", querier.profile)
	}
}

func hasCapability(device agentapi.DeviceReport, name, status string) bool {
	for _, capability := range device.Capabilities {
		if capability.Capability == name && capability.Status == status {
			return true
		}
	}
	return false
}

func writeUSBDevice(t *testing.T, root, name string, attributes map[string]string) {
	t.Helper()
	path := filepath.Join(root, name)
	mustMkdir(t, path)
	for attribute, value := range attributes {
		if err := os.WriteFile(filepath.Join(path, attribute), []byte(value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeInterface(t *testing.T, root, name string, number int, class, subclass, protocol, driver, tty string) {
	t.Helper()
	path := filepath.Join(root, name)
	mustMkdir(t, path)
	attributes := map[string]string{
		"bInterfaceNumber": string([]byte("0123456789abcdef")[number : number+1]),
		"bInterfaceClass":  class, "bInterfaceSubClass": subclass, "bInterfaceProtocol": protocol,
	}
	for attribute, value := range attributes {
		if err := os.WriteFile(filepath.Join(path, attribute), []byte(value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	driverRoot := filepath.Join(root, "drivers", driver)
	mustMkdir(t, driverRoot)
	if err := os.Symlink(driverRoot, filepath.Join(path, "driver")); err != nil {
		t.Fatal(err)
	}
	if tty != "" {
		mustMkdir(t, filepath.Join(path, tty))
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}
