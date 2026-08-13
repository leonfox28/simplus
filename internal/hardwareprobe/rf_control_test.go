package hardwareprobe

import (
	"context"
	"errors"
	"testing"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
)

type rfControlQuerier struct {
	probe     agentapi.DeviceProbe
	profile   string
	enabled   *bool
	setResult agentapi.RFObservation
	setError  error
}

func (querier *rfControlQuerier) Probe(context.Context, string, modemadapter.Adapter) agentapi.DeviceProbe {
	return querier.probe
}

func (querier *rfControlQuerier) SetRFState(_ context.Context, _ string, adapter modemadapter.RFControlAdapter, enabled bool) (agentapi.RFObservation, error) {
	querier.profile = adapter.Profile()
	querier.enabled = &enabled
	return querier.setResult, querier.setError
}

func TestML307ARFControlAdapterUsesFixedCommandsAndReadBack(t *testing.T) {
	zero := 0
	querier := &rfControlQuerier{
		probe: agentapi.DeviceProbe{DeviceID: "usb-1-3", State: agentapi.ProbeStateComplete,
			RF: agentapi.RFObservation{State: agentapi.RFStateOff}, ActiveCallCount: &zero},
		setResult: agentapi.RFObservation{State: agentapi.RFStateOn},
	}
	scanner := NewScanner()
	scanner.Querier = querier
	snapshot := agentapi.Snapshot{Devices: []agentapi.DeviceReport{{
		ID: "usb-1-3", Profile: agentapi.ProfileML307A,
		Interfaces: []agentapi.USBInterface{{Number: 2, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, Node: "/dev/fixture-ml307a"}}}},
	}}}
	observed, applied, err := scanner.SetRFState(t.Context(), snapshot, "usb-1-3", true)
	if err != nil || !applied || observed.State != agentapi.RFStateOn || querier.enabled == nil || !*querier.enabled || querier.profile != agentapi.ProfileML307A {
		t.Fatalf("observed=%#v applied=%v enabled=%v profile=%q error=%v", observed, applied, querier.enabled, querier.profile, err)
	}

	querier.probe.RF.State = agentapi.RFStateOn
	querier.enabled = nil
	observed, applied, err = scanner.SetRFState(t.Context(), snapshot, "usb-1-3", true)
	if err != nil || applied || observed.State != agentapi.RFStateOn || querier.enabled != nil {
		t.Fatalf("idempotent observed=%#v applied=%v enabled=%v error=%v", observed, applied, querier.enabled, err)
	}
}

func TestML307ARFControlRejectsActiveCallBeforeDispatch(t *testing.T) {
	one := 1
	querier := &rfControlQuerier{probe: agentapi.DeviceProbe{
		DeviceID: "usb-1-3", State: agentapi.ProbeStateComplete,
		RF: agentapi.RFObservation{State: agentapi.RFStateOn}, ActiveCallCount: &one,
	}}
	scanner := NewScanner()
	scanner.Querier = querier
	snapshot := agentapi.Snapshot{Devices: []agentapi.DeviceReport{{
		ID: "usb-1-3", Profile: agentapi.ProfileML307A,
		Interfaces: []agentapi.USBInterface{{Number: 2, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, Node: "/dev/fixture-ml307a"}}}},
	}}}
	if _, _, err := scanner.SetRFState(t.Context(), snapshot, "usb-1-3", false); !errors.Is(err, agentapi.ErrRFActiveCall) || querier.enabled != nil {
		t.Fatalf("error=%v enabled=%v", err, querier.enabled)
	}
}

func TestRFControlRevalidatesDeviceGenerationAfterGate(t *testing.T) {
	zero := 0
	querier := &rfControlQuerier{
		probe: agentapi.DeviceProbe{DeviceID: "usb-1-3", State: agentapi.ProbeStateComplete,
			RF: agentapi.RFObservation{State: agentapi.RFStateOff}, ActiveCallCount: &zero},
		setResult: agentapi.RFObservation{State: agentapi.RFStateOn},
	}
	scanner := NewScanner()
	scanner.Querier = querier
	snapshot := agentapi.Snapshot{Devices: []agentapi.DeviceReport{{
		ID: "usb-1-3", Generation: 4, Profile: agentapi.ProfileML307A,
		Interfaces: []agentapi.USBInterface{{Number: 2, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, Node: "/dev/fixture-ml307a"}}}},
	}}}
	changed := snapshot
	changed.Devices = append([]agentapi.DeviceReport(nil), snapshot.Devices...)
	changed.Devices[0].Generation++
	scanner.CurrentSnapshot = func() agentapi.Snapshot { return changed }
	if _, applied, err := scanner.SetRFState(t.Context(), snapshot, "usb-1-3", true); !errors.Is(err, agentapi.ErrRFDeviceStale) || applied || querier.enabled != nil {
		t.Fatalf("applied=%t enabled=%v error=%v", applied, querier.enabled, err)
	}
}
