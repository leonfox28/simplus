package connectivityagent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	app "github.com/leonfox28/simplus/internal/application/connectivity"
)

type fixtureClient struct{ response agentapi.ProbeResponse }

func (c fixtureClient) Snapshot(context.Context, bool) (agentapi.Snapshot, error) {
	return agentapi.Snapshot{}, nil
}
func (c fixtureClient) Probe(context.Context, agentapi.ProbeRequest) (agentapi.ProbeResponse, error) {
	return c.response, nil
}
func TestOnlyConfirmedRegistrationAndLossProduceObservations(t *testing.T) {
	target := app.ProbeTarget{Instance: "01234567-89ab-cdef-0123-456789abcdef", ID: "usb-1-3", Generation: 1, Revision: strings.Repeat("a", 64)}
	for _, test := range []struct {
		name, state, sim, rf, registration string
		known, connected                   bool
	}{
		{"home", agentapi.ProbeStateComplete, agentapi.SIMStatePresent, agentapi.RFStateOn, agentapi.RegistrationRegisteredHome, true, true},
		{"roaming", agentapi.ProbeStateComplete, agentapi.SIMStatePresent, agentapi.RFStateOn, agentapi.RegistrationRegisteredRoaming, true, true},
		{"searching", agentapi.ProbeStateComplete, agentapi.SIMStatePresent, agentapi.RFStateOn, agentapi.RegistrationSearching, true, false},
		{"unknown", agentapi.ProbeStateComplete, agentapi.SIMStatePresent, agentapi.RFStateOn, agentapi.RegistrationUnknown, false, false},
		{"timeout", agentapi.ProbeStateFailed, agentapi.SIMStateAbsent, agentapi.RFStateOff, agentapi.RegistrationUnknown, false, false},
		{"SIM absent", agentapi.ProbeStateComplete, agentapi.SIMStateAbsent, agentapi.RFStateOn, agentapi.RegistrationUnknown, true, false},
		{"RF off", agentapi.ProbeStateComplete, agentapi.SIMStatePresent, agentapi.RFStateOff, agentapi.RegistrationUnknown, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := agentapi.DeviceProbe{DeviceID: target.ID, State: test.state, SIM: agentapi.SIMObservation{State: test.sim, PrimaryLockState: agentapi.PrimaryLockReady}, RF: agentapi.RFObservation{State: test.rf}, Registrations: []agentapi.RegistrationObservation{{Domain: agentapi.RegistrationDomainCS, State: agentapi.RegistrationUnknown}, {Domain: agentapi.RegistrationDomainPacket, State: agentapi.RegistrationUnknown}, {Domain: agentapi.RegistrationDomainEPS, State: test.registration}}}
			probe.Identity.EquipmentIdentityFingerprint = strings.Repeat("b", 64)
			source := Source{Client: fixtureClient{agentapi.ProbeResponse{AgentInstanceID: target.Instance, SnapshotGeneration: target.Generation, SnapshotRevision: target.Revision, ObservedAt: time.Now(), Devices: []agentapi.DeviceProbe{probe}}}}
			sample, err := source.Probe(t.Context(), target)
			if err != nil || sample.Known != test.known || sample.Connected != test.connected {
				t.Fatalf("sample=%+v err=%v", sample, err)
			}
			stale := target
			stale.Instance = "fedcba98-7654-3210-fedc-ba9876543210"
			if _, err := source.Probe(t.Context(), stale); err == nil {
				t.Fatal("stale instance accepted")
			}
		})
	}
}
