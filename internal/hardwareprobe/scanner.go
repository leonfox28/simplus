package hardwareprobe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
)

var fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type ModemQuerier interface {
	Probe(context.Context, string, modemadapter.Adapter) agentapi.DeviceProbe
}

type smsCallSafetyQuerier interface {
	ReadSMSBlockingCallCount(context.Context, string, modemadapter.SMSCallSafetyAdapter) (int, bool)
}

type IdentityPseudonymizer = modemadapter.IdentityPseudonymizer

type Scanner struct {
	USBRoot    string
	DevRoot    string
	Querier    ModemQuerier
	Adapters   *modemadapter.Registry
	Identities IdentityPseudonymizer

	Gate            *OperationGate
	CurrentSnapshot func() agentapi.Snapshot
}

func (scanner *Scanner) readSMSBlockingCallCount(
	ctx context.Context,
	endpoint string,
	adapter modemadapter.SMSCallSafetyAdapter,
) (int, bool) {
	querier, ok := scanner.Querier.(smsCallSafetyQuerier)
	if !ok {
		return 0, false
	}
	return querier.ReadSMSBlockingCallCount(ctx, endpoint, adapter)
}

func NewScanner() *Scanner {
	return &Scanner{
		USBRoot: "/sys/bus/usb/devices", DevRoot: "/dev", Querier: NewATQuerier(),
		Adapters: modemadapter.DefaultRegistry(), Gate: NewOperationGate(),
	}
}

func (scanner *Scanner) Scan(ctx context.Context) ([]agentapi.DeviceReport, error) {
	if scanner == nil {
		return nil, errors.New("hardware scanner roots must be absolute")
	}
	return scanHardware(ctx, scanner.USBRoot, scanner.DevRoot, scanner.adapterRegistry(), scanner.Identities)
}

func scanHardware(ctx context.Context, usbRoot, devRoot string, registry *modemadapter.Registry, identities IdentityPseudonymizer) ([]agentapi.DeviceReport, error) {
	if !filepath.IsAbs(usbRoot) || !filepath.IsAbs(devRoot) || registry == nil {
		return nil, errors.New("hardware scanner roots and adapter registry are required")
	}
	entries, err := os.ReadDir(usbRoot)
	if err != nil {
		return nil, fmt.Errorf("read USB device tree: %w", err)
	}
	devices := make([]agentapi.DeviceReport, 0, 2)
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		name := entry.Name()
		if strings.Contains(name, ":") {
			continue
		}
		path := filepath.Join(usbRoot, name)
		vendorID, err := readAttribute(path, "idVendor", 16)
		if err != nil {
			continue
		}
		productID, err := readAttribute(path, "idProduct", 16)
		if err != nil {
			continue
		}
		manufacturer, _ := readAttribute(path, "manufacturer", 128)
		product, _ := readAttribute(path, "product", 128)
		adapter, ok := registry.Match(modemadapter.USBDescriptor{
			VendorID: vendorID, ProductID: productID, Manufacturer: manufacturer, Product: product,
		})
		if !ok {
			continue
		}
		device, err := scanDevice(usbRoot, devRoot, identities, name, path, adapter, strings.ToLower(vendorID), strings.ToLower(productID), manufacturer, product)
		if err != nil {
			return nil, fmt.Errorf("scan USB device %s: %w", name, err)
		}
		devices = append(devices, device)
	}
	sort.Slice(devices, func(left, right int) bool { return devices[left].ID < devices[right].ID })
	return devices, nil
}

func (scanner *Scanner) Probe(ctx context.Context, snapshot agentapi.Snapshot, requested []string) ([]agentapi.DeviceProbe, error) {
	if scanner == nil {
		return nil, errors.New("hardware scanner is unavailable")
	}
	selected := make([]string, 0, len(snapshot.Devices))
	requestedSet := make(map[string]bool, len(requested))
	for _, id := range requested {
		if requestedSet[id] {
			return nil, fmt.Errorf("duplicate requested device %q", id)
		}
		requestedSet[id] = true
	}
	for _, device := range snapshot.Devices {
		if len(requestedSet) == 0 || requestedSet[device.ID] {
			selected = append(selected, device.ID)
		}
	}
	if len(requestedSet) != 0 && len(selected) != len(requestedSet) {
		return nil, errors.New("requested device is not present in snapshot")
	}
	results := make([]agentapi.DeviceProbe, 0, len(selected))
	for _, id := range selected {
		release, err := scanner.operationGate().Acquire(ctx, id)
		if err != nil {
			return nil, err
		}
		current, _, currentErr := scanner.currentSnapshotDevice(snapshot, id)
		if currentErr != nil {
			release()
			return nil, currentErr
		}
		probes, probeErr := scanner.probeLocked(ctx, current, []string{id})
		release()
		if probeErr != nil {
			return nil, probeErr
		}
		results = append(results, probes...)
	}
	return results, nil
}

// currentSnapshotDevice revalidates a queued operation against the monitor's
// latest published device generation after the per-device gate is acquired.
// Focused adapter fixtures may omit CurrentSnapshot and use their supplied
// immutable snapshot directly.
func (scanner *Scanner) currentSnapshotDevice(expected agentapi.Snapshot, deviceID string) (agentapi.Snapshot, agentapi.DeviceReport, error) {
	var expectedDevice agentapi.DeviceReport
	expectedFound := false
	for _, device := range expected.Devices {
		if device.ID == deviceID {
			expectedDevice, expectedFound = device, true
			break
		}
	}
	if !expectedFound {
		return agentapi.Snapshot{}, agentapi.DeviceReport{}, errors.New("requested device is not present in snapshot")
	}
	current := expected
	if scanner != nil && scanner.CurrentSnapshot != nil {
		current = scanner.CurrentSnapshot()
	}
	for _, device := range current.Devices {
		if device.ID != deviceID {
			continue
		}
		if device.Generation != expectedDevice.Generation || device.Profile != expectedDevice.Profile {
			return agentapi.Snapshot{}, agentapi.DeviceReport{}, errors.New("requested device generation changed while queued")
		}
		return current, device, nil
	}
	return agentapi.Snapshot{}, agentapi.DeviceReport{}, errors.New("requested device disappeared while queued")
}

func (scanner *Scanner) operationGate() *OperationGate {
	if scanner.Gate == nil {
		scanner.Gate = NewOperationGate()
	}
	return scanner.Gate
}

func (scanner *Scanner) probeLocked(ctx context.Context, snapshot agentapi.Snapshot, requested []string) ([]agentapi.DeviceProbe, error) {
	selected := make(map[string]bool)
	if len(requested) != 0 {
		for _, id := range requested {
			if selected[id] {
				return nil, fmt.Errorf("duplicate requested device %q", id)
			}
			selected[id] = true
		}
	}
	known := make(map[string]agentapi.DeviceReport, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		known[device.ID] = device
	}
	for id := range selected {
		if _, ok := known[id]; !ok {
			return nil, fmt.Errorf("requested device %q is not present in snapshot generation %d", id, snapshot.Generation)
		}
	}
	results := make([]agentapi.DeviceProbe, 0, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		if len(selected) != 0 && !selected[device.ID] {
			continue
		}
		result := agentapi.DeviceProbe{
			DeviceID: device.ID, State: agentapi.ProbeStateDescriptorOnly,
			RF:             agentapi.RFObservation{State: agentapi.RFStateUnknown},
			SIM:            agentapi.SIMObservation{State: agentapi.SIMStateUnknown, PrimaryLockState: agentapi.PrimaryLockUnknown},
			SignalMetrics:  agentapi.SignalObservation{State: agentapi.SignalStateUnknown},
			Registrations:  []agentapi.RegistrationObservation{},
			CurrentNetwork: agentapi.NetworkObservation{SelectionMode: agentapi.NetworkSelectionUnknown},
		}
		endpoint := scanner.preferredATEndpoint(device)
		if endpoint == "" {
			result.State = agentapi.ProbeStateUnavailable
			result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerDevice, Code: agentapi.ErrorControlEndpointMissing, Retryable: true}
			result.ErrorCode = agentapi.ErrorControlEndpointMissing
			result.ErrorDetail = "preferred AT endpoint is unavailable"
			results = append(results, result)
			continue
		}
		if scanner.Querier == nil {
			result.State = agentapi.ProbeStateUnavailable
			result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerPlatform, Code: agentapi.ErrorPlatformUnsupported}
			result.ErrorCode = agentapi.ErrorPlatformUnsupported
			result.ErrorDetail = "AT probing is unavailable on this platform"
			results = append(results, result)
			continue
		}
		base, adapterOK := scanner.adapterRegistry().ForProfile(device.Profile)
		if !adapterOK {
			result.State = agentapi.ProbeStateUnavailable
			result.Error = &agentapi.ProbeError{Layer: agentapi.ErrorLayerPlatform, Code: agentapi.ErrorPlatformUnsupported}
			result.ErrorCode = agentapi.ErrorPlatformUnsupported
			result.ErrorDetail = "modem adapter is unavailable"
			results = append(results, result)
			continue
		}
		result = scanner.Querier.Probe(ctx, endpoint, base)
		result.DeviceID = device.ID
		results = append(results, result)
	}
	return results, nil
}

func scanDevice(usbRoot, devRoot string, identities IdentityPseudonymizer, name, path string, adapter modemadapter.Adapter, vendorID, productID, manufacturer, product string) (agentapi.DeviceReport, error) {
	bcdDevice, _ := readAttribute(path, "bcdDevice", 16)
	serial, serialErr := readAttribute(path, "serial", 128)
	serial = safeText(serial, 128)
	serialPresent := serialErr == nil && serial != ""
	if !serialPresent {
		serial = ""
	}
	serialFingerprint := ""
	if serialPresent && identities != nil {
		value := adapter.Profile() + "\x00" + vendorID + ":" + productID + "\x00" + serial
		if fingerprint, fingerprintErr := identities.Pseudonym("modem-usb-serial-v1", []byte(value)); fingerprintErr == nil && fingerprintPattern.MatchString(fingerprint) {
			serialFingerprint = fingerprint
		}
	}
	configurationText, _ := readAttribute(path, "bConfigurationValue", 16)
	configuration, _ := strconv.Atoi(configurationText)
	interfaces, err := scanInterfaces(usbRoot, devRoot, name, configuration)
	if err != nil {
		return agentapi.DeviceReport{}, err
	}
	device := agentapi.DeviceReport{
		ID: stableDeviceID(name), PhysicalPath: name, Profile: adapter.Profile(), DisplayName: adapter.DisplayName(),
		USB: agentapi.USBIdentity{
			VendorID: vendorID, ProductID: productID, BCDDevice: strings.ToLower(bcdDevice),
			Manufacturer: safeText(manufacturer, 128), Product: safeText(product, 128),
			SerialPresent: serialPresent, SerialNumber: serial, SerialFingerprint: serialFingerprint,
			Configuration: configuration, InterfaceCount: len(interfaces),
		},
		Interfaces: interfaces,
	}
	device.Capabilities = adapter.Capabilities(device)
	return device, nil
}

func scanInterfaces(usbRoot, devRoot, deviceName string, configuration int) ([]agentapi.USBInterface, error) {
	pattern := filepath.Join(usbRoot, deviceName+":*.*")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	interfaces := make([]agentapi.USBInterface, 0, len(paths))
	for _, path := range paths {
		numberText, err := readAttribute(path, "bInterfaceNumber", 16)
		if err != nil {
			continue
		}
		numberValue, err := strconv.ParseUint(numberText, 16, 8)
		if err != nil {
			continue
		}
		class, _ := readAttribute(path, "bInterfaceClass", 16)
		subclass, _ := readAttribute(path, "bInterfaceSubClass", 16)
		protocol, _ := readAttribute(path, "bInterfaceProtocol", 16)
		driver := symlinkBase(filepath.Join(path, "driver"))
		usbInterface := agentapi.USBInterface{
			Number: int(numberValue), Class: strings.ToLower(class), Subclass: strings.ToLower(subclass),
			Protocol: strings.ToLower(protocol), Driver: driver,
		}
		usbInterface.Endpoints = scanInterfaceEndpoints(devRoot, path, usbInterface.Number, driver)
		interfaces = append(interfaces, usbInterface)
	}
	sort.Slice(interfaces, func(left, right int) bool { return interfaces[left].Number < interfaces[right].Number })
	_ = configuration
	return interfaces, nil
}

func scanInterfaceEndpoints(devRoot, path string, interfaceNumber int, driver string) []agentapi.Endpoint {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	endpoints := make([]agentapi.Endpoint, 0, 3)
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, "ttyUSB") || strings.HasPrefix(name, "ttyACM"):
			endpoints = append(endpoints, agentapi.Endpoint{Kind: agentapi.EndpointTTY, InterfaceNumber: interfaceNumber, Driver: driver, Node: filepath.Join(devRoot, name)})
		case name == "usbmisc":
			children, _ := os.ReadDir(filepath.Join(path, name))
			for _, child := range children {
				if strings.HasPrefix(child.Name(), "cdc-wdm") {
					endpoints = append(endpoints, agentapi.Endpoint{Kind: agentapi.EndpointQMI, InterfaceNumber: interfaceNumber, Driver: driver, Node: filepath.Join(devRoot, child.Name())})
				}
			}
		case name == "net":
			children, _ := os.ReadDir(filepath.Join(path, name))
			if len(children) != 0 {
				// Interface names may embed a hardware MAC (for example enx...). Presence is enough for HIL-0.
				endpoints = append(endpoints, agentapi.Endpoint{Kind: agentapi.EndpointNet, InterfaceNumber: interfaceNumber, Driver: driver})
			}
		case name == "sound":
			children, _ := os.ReadDir(filepath.Join(path, name))
			for _, child := range children {
				if strings.HasPrefix(child.Name(), "card") {
					endpoints = append(endpoints, agentapi.Endpoint{Kind: agentapi.EndpointALSA, InterfaceNumber: interfaceNumber, Driver: driver, Node: child.Name()})
				}
			}
		}
	}
	sort.Slice(endpoints, func(left, right int) bool {
		if endpoints[left].Kind == endpoints[right].Kind {
			return endpoints[left].Node < endpoints[right].Node
		}
		return endpoints[left].Kind < endpoints[right].Kind
	})
	return endpoints
}

func stableDeviceID(physicalPath string) string {
	return "usb-" + strings.ReplaceAll(physicalPath, ".", "-port-")
}

func (scanner *Scanner) preferredATEndpoint(device agentapi.DeviceReport) string {
	adapter, ok := scanner.adapterRegistry().ForProfile(device.Profile)
	if !ok {
		return ""
	}
	endpoint, ok := adapter.Endpoint(device, modemadapter.EndpointPrimaryAT)
	if !ok {
		return ""
	}
	return endpoint.Node
}

func (scanner *Scanner) adapterRegistry() *modemadapter.Registry {
	if scanner != nil && scanner.Adapters != nil {
		return scanner.Adapters
	}
	return modemadapter.DefaultRegistry()
}

func readAttribute(path, name string, limit int64) (string, error) {
	file, err := os.Open(filepath.Join(path, name))
	if err != nil {
		return "", err
	}
	defer file.Close()
	buffer := make([]byte, limit+1)
	count, err := file.Read(buffer)
	if err != nil {
		return "", err
	}
	if int64(count) > limit {
		return "", errors.New("attribute exceeds size limit")
	}
	return strings.TrimSpace(string(buffer[:count])), nil
}

func symlinkBase(path string) string {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

func safeText(value string, limit int) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}
