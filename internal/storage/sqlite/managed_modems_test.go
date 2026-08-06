package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/hardware"
	domain "github.com/leonfox28/simplus/internal/domain/modem"
)

func TestManagedModemPersistsCapabilitiesAndUniqueEquipmentIdentity(t *testing.T) {
	set, err := OpenSet(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	now := time.Date(2026, 8, 5, 13, 0, 0, 123, time.UTC)
	record := domain.Record{
		ID: "modem_AQEBAQEBAQEBAQEBAQEBAQ", EquipmentIdentityFingerprint: strings.Repeat("a", 64),
		USBSerialFingerprint: strings.Repeat("b", 64),
		DisplayName:          "Main modem", Model: "ML307A", Transport: hardware.TransportUSB,
		Capabilities: hardware.Capabilities{SIMAccess: true, HostVoWiFiAuth: true, RFControl: true},
		CreatedAt:    now, UpdatedAt: now,
	}
	if err := set.CreateManagedModem(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	items, err := set.ListManagedModems(t.Context())
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v error=%v", items, err)
	}
	if items[0].EquipmentIdentityFingerprint != record.EquipmentIdentityFingerprint ||
		items[0].USBSerialFingerprint != record.USBSerialFingerprint || items[0].LegacyHardwareDeviceID != "" ||
		!items[0].Capabilities.HostVoWiFiAuth || !items[0].Capabilities.RFControl || !items[0].CreatedAt.Equal(now) {
		t.Fatalf("stored=%#v", items[0])
	}
	record.ID = "modem_AgICAgICAgICAgICAgICAg"
	if err := set.CreateManagedModem(t.Context(), record); err == nil {
		t.Fatal("duplicate equipment identity was accepted")
	}
}

func TestManagedModemLegacyBindingIsPromotedAtomically(t *testing.T) {
	set, err := OpenSet(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	now := time.Date(2026, 8, 5, 13, 0, 0, 0, time.UTC)
	record := domain.Record{
		ID: "modem_AQEBAQEBAQEBAQEBAQEBAQ", LegacyHardwareDeviceID: "agent-usb-1-3",
		DisplayName: "ML307A", Model: "ML307A", Transport: hardware.TransportUSB,
		Capabilities: hardware.Capabilities{SIMAccess: true}, CreatedAt: now, UpdatedAt: now,
	}
	if err := set.CreateManagedModem(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	identity := strings.Repeat("c", 64)
	serial := strings.Repeat("d", 64)
	if err := set.BindManagedModemIdentity(t.Context(), record.ID, identity, serial, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	items, err := set.ListManagedModems(t.Context())
	if err != nil || len(items) != 1 || items[0].LegacyHardwareDeviceID != "" ||
		items[0].EquipmentIdentityFingerprint != identity || items[0].USBSerialFingerprint != serial {
		t.Fatalf("promoted items=%#v error=%v", items, err)
	}
}
