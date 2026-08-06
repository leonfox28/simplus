package accesspath_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	app "github.com/leonfox28/simplus/internal/application/accesspath"
	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/accessmode"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	linedomain "github.com/leonfox28/simplus/internal/domain/line"
	modemdomain "github.com/leonfox28/simplus/internal/domain/modem"
	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
)

const stableLineID = "line_AQEBAQEBAQEBAQEBAQEBAQ"

type fixedLines struct{}

func (fixedLines) Topology(context.Context) (inventory.Topology, error) {
	return inventory.Topology{Lines: []inventory.Line{{ID: stableLineID, State: inventory.LineReady}}}, nil
}

func TestMihomoRequiredFailsClosedWithoutHardCodedSimulatorLine(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	now := time.Date(2026, 8, 5, 15, 0, 0, 0, time.UTC)
	modemID := "modem_AQEBAQEBAQEBAQEBAQEBAQ"
	if err := stores.CreateManagedModem(ctx, modemdomain.Record{
		ID: modemID, EquipmentIdentityFingerprint: strings.Repeat("a", 64), DisplayName: "Simulator modem",
		Model: "Simulator", Transport: hardware.TransportSimulated, Capabilities: hardware.Capabilities{SIMAccess: true},
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := stores.CreateManagedLine(ctx, linedomain.Record{
		ID: stableLineID, ManagedModemID: modemID, SIMSlotIndex: 0,
		SubscriptionIdentityFingerprint: strings.Repeat("b", 64), SubscriptionDisplayHint: "SIM •••• 0101",
		DisplayName: "Simulator Line", AccessMode: accessmode.HostVoWiFiOnly, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	service, err := app.New(stores, fixedLines{})
	if err != nil {
		t.Fatal(err)
	}
	states, err := service.List(ctx)
	if err != nil || len(states) != 1 || states[0].LineID != stableLineID || states[0].LineState != "online" {
		t.Fatalf("defaults=%#v err=%v", states, err)
	}
	state, err := service.Configure(ctx, stableLineID, "mihomo-required", "failed")
	if err != nil || state.LineState != "offline" || state.DirectFallback || state.EPDG != "blocked" {
		t.Fatalf("failed=%#v err=%v", state, err)
	}
	state, err = service.Configure(ctx, stableLineID, "mihomo-required", "running")
	if err != nil || state.LineState != "online" || state.IMS != "registered" || !service.Available(ctx, stableLineID) {
		t.Fatalf("running=%#v err=%v", state, err)
	}
	if _, err := service.Configure(ctx, "simulator-line-1", "direct", "stopped"); err == nil {
		t.Fatal("legacy hard-coded Line was accepted")
	}
}
