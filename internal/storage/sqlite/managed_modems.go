package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/leonfox28/simplus/internal/domain/hardware"
	domain "github.com/leonfox28/simplus/internal/domain/modem"
)

func (set *Set) ListManagedModems(ctx context.Context) ([]domain.Record, error) {
	if set == nil || set.Core == nil {
		return nil, fmt.Errorf("core database is not open")
	}
	rows, err := set.Core.QueryContext(ctx, `
SELECT id, legacy_hardware_device_id, equipment_identity_fingerprint, usb_serial_fingerprint,
       display_name, model, transport, capability_mask, created_at_utc, updated_at_utc
FROM managed_modems
ORDER BY created_at_utc, id`)
	if err != nil {
		return nil, fmt.Errorf("query managed modems: %w", err)
	}
	defer rows.Close()
	result := []domain.Record{}
	for rows.Next() {
		var record domain.Record
		var mask int64
		var created, updated string
		if err := rows.Scan(&record.ID, &record.LegacyHardwareDeviceID, &record.EquipmentIdentityFingerprint,
			&record.USBSerialFingerprint, &record.DisplayName, &record.Model, &record.Transport, &mask, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan managed modem: %w", err)
		}
		record.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("parse managed modem created time: %w", err)
		}
		record.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("parse managed modem updated time: %w", err)
		}
		record.Capabilities = decodeModemCapabilities(mask)
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate managed modems: %w", err)
	}
	return result, nil
}

func (set *Set) CreateManagedModem(ctx context.Context, record domain.Record) error {
	if set == nil || set.Core == nil {
		return fmt.Errorf("core database is not open")
	}
	_, err := set.Core.ExecContext(ctx, `
INSERT INTO managed_modems (
  id, legacy_hardware_device_id, equipment_identity_fingerprint, usb_serial_fingerprint,
  display_name, model, transport, capability_mask, created_at_utc, updated_at_utc
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.ID, record.LegacyHardwareDeviceID,
		record.EquipmentIdentityFingerprint, record.USBSerialFingerprint, record.DisplayName, record.Model,
		record.Transport, encodeModemCapabilities(record.Capabilities), record.CreatedAt.UTC().Format(time.RFC3339Nano),
		record.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert managed modem: %w", err)
	}
	return nil
}

func (set *Set) BindManagedModemIdentity(ctx context.Context, modemID, equipmentFingerprint, usbSerialFingerprint string, updatedAt time.Time) error {
	if set == nil || set.Core == nil {
		return fmt.Errorf("core database is not open")
	}
	result, err := set.Core.ExecContext(ctx, `
UPDATE managed_modems
SET legacy_hardware_device_id = '', equipment_identity_fingerprint = ?, usb_serial_fingerprint = ?, updated_at_utc = ?
WHERE id = ? AND (equipment_identity_fingerprint = '' OR equipment_identity_fingerprint = ?)`,
		equipmentFingerprint, usbSerialFingerprint, updatedAt.UTC().Format(time.RFC3339Nano), modemID, equipmentFingerprint)
	if err != nil {
		return fmt.Errorf("bind managed modem equipment identity: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read managed modem identity binding result: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("managed modem identity binding did not update exactly one record")
	}
	return nil
}

func encodeModemCapabilities(value hardware.Capabilities) int64 {
	flags := []bool{
		value.SIMAccess, value.SMS, value.CellularVoice, value.DigitalVoiceMedia, value.USBUAC,
		value.SIMAPDU, value.HostVoWiFiAuth, value.RFControl, value.NetworkScan,
		value.ManualNetworkSelection, value.PrimarySIMLockState, value.PIN1Verify,
		value.PUK1Unblock, value.EUICCProfiles,
	}
	var result int64
	for index, enabled := range flags {
		if enabled {
			result |= 1 << index
		}
	}
	return result
}

func decodeModemCapabilities(mask int64) hardware.Capabilities {
	enabled := func(index uint) bool { return mask&(1<<index) != 0 }
	return hardware.Capabilities{
		SIMAccess: enabled(0), SMS: enabled(1), CellularVoice: enabled(2), DigitalVoiceMedia: enabled(3),
		USBUAC: enabled(4), SIMAPDU: enabled(5), HostVoWiFiAuth: enabled(6), RFControl: enabled(7),
		NetworkScan: enabled(8), ManualNetworkSelection: enabled(9), PrimarySIMLockState: enabled(10),
		PIN1Verify: enabled(11), PUK1Unblock: enabled(12), EUICCProfiles: enabled(13),
	}
}
