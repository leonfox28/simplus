package hardwareprobe

import (
	"context"
	"strings"
	"testing"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
)

type equipmentIdentityQuerier struct {
	profile     string
	endpoint    string
	observation agentapi.EquipmentIdentityObservation
}

func (querier *equipmentIdentityQuerier) Probe(context.Context, string, modemadapter.Adapter) agentapi.DeviceProbe {
	return agentapi.DeviceProbe{}
}

func (querier *equipmentIdentityQuerier) ReadEquipmentIdentity(_ context.Context, endpoint string, adapter modemadapter.EquipmentIdentityAdapter) (agentapi.EquipmentIdentityObservation, error) {
	querier.endpoint = endpoint
	querier.profile = adapter.Profile()
	return querier.observation, nil
}

func TestScannerReadsEquipmentIdentityThroughTheSelectedModelAdapter(t *testing.T) {
	querier := &equipmentIdentityQuerier{observation: agentapi.EquipmentIdentityObservation{
		IMEI: "490154203237518", Fingerprint: strings.Repeat("a", 64),
	}}
	scanner := NewScanner()
	scanner.Querier = querier
	snapshot := agentapi.Snapshot{Devices: []agentapi.DeviceReport{{
		ID: "usb-1-3", Profile: agentapi.ProfileML307A,
		Interfaces: []agentapi.USBInterface{{Number: 2, Endpoints: []agentapi.Endpoint{{Kind: agentapi.EndpointTTY, Node: "/dev/fixture-ml307a"}}}},
	}}}
	observation, err := scanner.ReadEquipmentIdentity(t.Context(), snapshot, "usb-1-3")
	if err != nil || observation.IMEI != "490154203237518" || querier.profile != agentapi.ProfileML307A || querier.endpoint != "/dev/fixture-ml307a" {
		t.Fatalf("observation=%#v profile=%q endpoint=%q error=%v", observation, querier.profile, querier.endpoint, err)
	}
}
