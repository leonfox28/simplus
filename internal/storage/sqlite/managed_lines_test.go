package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/accessmode"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	linedomain "github.com/leonfox28/simplus/internal/domain/line"
	modemdomain "github.com/leonfox28/simplus/internal/domain/modem"
)

func TestManagedLinePersistsStableModemAndSubscriptionBinding(t *testing.T) {
	set, err := OpenSet(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	now := time.Date(2026, 8, 5, 14, 0, 0, 123, time.UTC)
	modem := modemdomain.Record{
		ID: "modem_AQEBAQEBAQEBAQEBAQEBAQ", EquipmentIdentityFingerprint: strings.Repeat("a", 64),
		DisplayName: "Main modem", Model: "ML307A", Transport: hardware.TransportUSB,
		Capabilities: hardware.Capabilities{SIMAccess: true, HostVoWiFiAuth: true}, CreatedAt: now, UpdatedAt: now,
	}
	if err := set.CreateManagedModem(t.Context(), modem); err != nil {
		t.Fatal(err)
	}
	record := linedomain.Record{
		ID: "line_AgICAgICAgICAgICAgICAg", ManagedModemID: modem.ID, SIMSlotIndex: 0,
		SubscriptionIdentityFingerprint: strings.Repeat("b", 64), SubscriptionDisplayHint: "ICCID •••• 1234",
		DisplayName: "VOXI", AccessMode: accessmode.HostVoWiFiOnly, CreatedAt: now, UpdatedAt: now,
	}
	if err := set.CreateManagedLine(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	items, err := set.ListManagedLines(t.Context())
	if err != nil || len(items) != 1 || items[0] != record {
		t.Fatalf("items=%#v error=%v", items, err)
	}
	if err := set.UpdateManagedLine(t.Context(), record.ID, "VOXI UK", accessmode.HoldRFOff, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	items, err = set.ListManagedLines(t.Context())
	if err != nil || items[0].DisplayName != "VOXI UK" || items[0].AccessMode != accessmode.HoldRFOff {
		t.Fatalf("updated=%#v error=%v", items, err)
	}
	if err := set.UpdateManagedLine(t.Context(), "line_missing0123456789012", "missing", accessmode.HoldRFOff, now); !errors.Is(err, linedomain.ErrNotFound) {
		t.Fatalf("missing update error=%v", err)
	}
	record.ID = "line_AwMDAwMDAwMDAwMDAwMDAw"
	if err := set.CreateManagedLine(t.Context(), record); err == nil {
		t.Fatal("duplicate modem/subscription binding was accepted")
	}
}
