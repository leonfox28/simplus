package hardwareprobe

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leonfox28/simplus/internal/agentapi"
)

type fakeQuerier struct {
	endpoint string
	profile  string
	commands int
}

func (querier *fakeQuerier) Probe(_ context.Context, endpoint, profile string) agentapi.DeviceProbe {
	querier.endpoint = endpoint
	querier.profile = profile
	count := 0
	return agentapi.DeviceProbe{
		State:           agentapi.ProbeStateComplete,
		RF:              agentapi.RFObservation{State: agentapi.RFStateOff},
		SIM:             agentapi.SIMObservation{State: agentapi.SIMStateAbsent, PrimaryLockState: agentapi.PrimaryLockUnknown},
		SignalMetrics:   agentapi.SignalObservation{State: agentapi.SignalStateUnknown},
		Registrations:   []agentapi.RegistrationObservation{},
		CurrentNetwork:  agentapi.NetworkObservation{SelectionMode: agentapi.NetworkSelectionUnknown},
		ActiveCallCount: &count,
	}
}

func (querier *fakeQuerier) EnsureRadioOff(_ context.Context, endpoint, profile string) agentapi.RadioEnsureOffExecution {
	querier.endpoint = endpoint
	querier.profile = profile
	querier.commands++
	count := 0
	return agentapi.RadioEnsureOffExecution{
		Observation: agentapi.RadioEnsureOffObservation{
			RF: agentapi.RFObservation{State: agentapi.RFStateOff}, ActiveCallCount: &count,
		},
	}
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

	querier := &fakeQuerier{}
	scanner := &Scanner{USBRoot: usbRoot, DevRoot: devRoot, Querier: querier}
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
	if querier.endpoint != filepath.Join(devRoot, "ttyUSB2") || querier.profile != agentapi.ProfileQDC507 {
		t.Fatalf("query = endpoint %q profile %q", querier.endpoint, querier.profile)
	}
	if _, err := scanner.Probe(context.Background(), snapshot, []string{"missing"}); err == nil {
		t.Fatal("missing device probe unexpectedly succeeded")
	}
}

func TestScannerReportsMissingPreferredQDC507EndpointAsRetryableDeviceError(t *testing.T) {
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

func TestScannerDescriptorOnlyProbeUsesTypedDefaults(t *testing.T) {
	scanner := &Scanner{USBRoot: "/unused", DevRoot: "/dev", Querier: &fakeQuerier{}}
	snapshot := agentapi.Snapshot{Generation: 3, Devices: []agentapi.DeviceReport{{
		ID: "usb-1-3", Profile: agentapi.ProfileML307A,
	}}}
	probes, err := scanner.Probe(context.Background(), snapshot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 || probes[0].State != agentapi.ProbeStateDescriptorOnly ||
		probes[0].SIM.PrimaryLockState != agentapi.PrimaryLockUnknown || probes[0].Registrations == nil {
		t.Fatalf("descriptor-only probe = %#v", probes)
	}
}

func TestScannerRoutesEnsureRadioOffOnlyToPreferredQDC507Endpoint(t *testing.T) {
	querier := &fakeQuerier{}
	scanner := &Scanner{USBRoot: "/unused", DevRoot: "/dev", Querier: querier}
	snapshot := agentapi.Snapshot{Generation: 9, Devices: []agentapi.DeviceReport{{
		ID: "usb-1-1", Profile: agentapi.ProfileQDC507,
		Interfaces: []agentapi.USBInterface{{
			Number: 2,
			Endpoints: []agentapi.Endpoint{{
				Kind: agentapi.EndpointTTY, InterfaceNumber: 2, Node: "/dev/ttyUSB2",
			}},
		}},
	}}}
	execution, err := scanner.EnsureRadioOff(context.Background(), snapshot, "usb-1-1")
	if err != nil || execution.Error != nil || execution.Observation.RF.State != agentapi.RFStateOff {
		t.Fatalf("execution=%#v err=%v", execution, err)
	}
	if querier.commands != 1 || querier.endpoint != "/dev/ttyUSB2" || querier.profile != agentapi.ProfileQDC507 {
		t.Fatalf("commands=%d endpoint=%q profile=%q", querier.commands, querier.endpoint, querier.profile)
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
