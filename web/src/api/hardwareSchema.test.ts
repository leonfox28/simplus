import { describe, expect, it } from 'vitest'
import { isHardwareTopologyResponse } from './hardwareSchema'

const capabilities = {
  simAccess: true,
  sms: false,
  cellularVoice: false,
  digitalVoiceMedia: false,
  usbUac: false,
  simApdu: false,
  hostVoWifiAuth: false,
  rfControl: false,
  networkScan: false,
  manualNetworkSelection: false,
  primarySimLockState: false,
  pin1Verify: false,
  puk1Unblock: false,
  euiccProfiles: false,
}

function topology() {
  return {
    generation: 1,
    revision: 'a'.repeat(64),
    observedAt: '2026-08-07T00:00:00Z',
    devices: [{ id: 'device-1', displayName: 'Synthetic Device', transport: 'simulated', state: 'available', generation: 1 }],
    modemFunctions: [{ id: 'function-1', physicalDeviceId: 'device-1', displayName: 'Synthetic Modem', backend: 'simulated', generation: 1, capabilities }],
    simSlots: [{ id: 'slot-1', physicalDeviceId: 'device-1', index: 0, presence: 'present', activeMediaId: 'media-1', generation: 1 }],
    simMedia: [{ id: 'media-1', simSlotId: 'slot-1', kind: 'uicc', identityState: 'known', displayIdentityHint: 'SIM •••• 0001', generation: 1 }],
    subscriptionProfiles: [{ id: 'profile-1', simMediaId: 'media-1', displayName: 'Synthetic SIM', state: 'active', displayIdentityHint: 'Profile •••• 0001', generation: 1 }],
    resourceGroups: [{
      id: 'group-1', physicalDeviceId: 'device-1', displayName: 'Synthetic Group', resources: ['sim-access'],
      modemFunctionIds: ['function-1'], simSlotIds: ['slot-1'], maxActiveCalls: 0, maxConcurrentOps: 1, generation: 1,
    }],
    lines: [{
      id: 'line-1', physicalDeviceId: 'device-1', modemFunctionId: 'function-1', subscriptionProfileId: 'profile-1',
      resourceGroupId: 'group-1', displayName: 'Synthetic Line', generation: 1, capabilities, state: 'ready',
    }],
  }
}

describe('hardware topology boundary', () => {
  it('accepts a coherent capability graph', () => {
    expect(isHardwareTopologyResponse(topology())).toBe(true)
  })

  it('rejects broken cross-references and unexpected private fields', () => {
    const brokenReference = topology()
    brokenReference.lines[0]!.modemFunctionId = 'missing-function'
    expect(isHardwareTopologyResponse(brokenReference)).toBe(false)

    const privateField = topology() as ReturnType<typeof topology> & { rawDevicePath?: string }
    privateField.rawDevicePath = '/private/device/path'
    expect(isHardwareTopologyResponse(privateField)).toBe(false)
  })
})
