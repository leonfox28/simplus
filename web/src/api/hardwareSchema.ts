import type { components } from './schema'

export type HardwareTopologyResponse = components['schemas']['HardwareTopologyResponse']
type HardwareCapabilities = components['schemas']['HardwareCapabilities']
type PhysicalDeviceDetail = components['schemas']['PhysicalDeviceDetail']
type ModemFunctionDetail = components['schemas']['ModemFunctionDetail']
type SIMSlotDetail = components['schemas']['SIMSlotDetail']
type SIMMediaDetail = components['schemas']['SIMMediaDetail']
type SubscriptionProfileDetail = components['schemas']['SubscriptionProfileDetail']
type ResourceGroupDetail = components['schemas']['ResourceGroupDetail']
type ResourceKind = components['schemas']['ResourceKind']
type HardwareLineDetail = components['schemas']['HardwareLineDetail']

const idPattern = /^[a-z0-9][a-z0-9-]{0,63}$/
const resourceKinds = new Set([
  'radio-control',
  'sim-access',
  'voice-media',
  'sms-storage',
  'sim-apdu',
  'host-vowifi-auth',
  'network-selection',
  'sim-lock',
  'euicc-profiles',
])

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasExactKeys(value: Record<string, unknown>, expected: readonly string[]): boolean {
  const keys = Object.keys(value)
  return keys.length === expected.length && keys.every((key) => expected.includes(key))
}

function isGeneration(value: unknown): value is number {
  return Number.isSafeInteger(value) && (value as number) > 0
}

function isID(value: unknown): value is string {
  return typeof value === 'string' && idPattern.test(value)
}

function isLabel(value: unknown, maximum = 256): value is string {
  return (
    typeof value === 'string' &&
    value.length > 0 &&
    value.length <= maximum &&
    value.trim() === value &&
    !/[\u0000-\u001f\u007f]/.test(value)
  )
}

export function isHardwareCapabilities(value: unknown): value is HardwareCapabilities {
  if (!isRecord(value)) return false
  const capabilityKeys = [
    'simAccess',
    'sms',
    'cellularVoice',
    'digitalVoiceMedia',
    'usbUac',
    'simApdu',
    'hostVoWifiAuth',
    'rfControl',
    'networkScan',
    'manualNetworkSelection',
    'primarySimLockState',
    'pin1Verify',
    'puk1Unblock',
    'euiccProfiles',
  ] as const
  if (!hasExactKeys(value, capabilityKeys) || !capabilityKeys.every((key) => typeof value[key] === 'boolean')) return false
  return Boolean(
    (!value.usbUac || (value.digitalVoiceMedia && value.cellularVoice)) &&
    (!value.simApdu || value.simAccess) &&
    (!value.hostVoWifiAuth || value.simApdu) &&
    (!(value.primarySimLockState || value.pin1Verify || value.puk1Unblock) || value.simAccess) &&
    (!value.euiccProfiles || value.simApdu)
  )
}

function capabilitiesSubset(candidate: HardwareCapabilities, available: HardwareCapabilities): boolean {
  return (Object.keys(candidate) as Array<keyof HardwareCapabilities>).every(
    (key) => !candidate[key] || available[key],
  )
}

function resourceSupported(resource: ResourceKind, boundFunctions: ModemFunctionDetail[]): boolean {
  return boundFunctions.some(({ capabilities }) => {
    switch (resource) {
      case 'sim-access': return capabilities.simAccess
      case 'radio-control': return capabilities.rfControl
      case 'voice-media': return capabilities.cellularVoice && capabilities.digitalVoiceMedia
      case 'sms-storage': return capabilities.sms
      case 'sim-apdu': return capabilities.simApdu
      case 'host-vowifi-auth': return capabilities.hostVoWifiAuth
      case 'network-selection': return capabilities.networkScan || capabilities.manualNetworkSelection
      case 'sim-lock': return capabilities.primarySimLockState || capabilities.pin1Verify || capabilities.puk1Unblock
      case 'euicc-profiles': return capabilities.euiccProfiles
      default: return false
    }
  })
}

function isDevice(value: unknown): value is PhysicalDeviceDetail {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'displayName', 'transport', 'state', 'generation'])) return false
  return (
    isID(value.id) &&
    isLabel(value.displayName) &&
    (value.transport === 'simulated' || value.transport === 'usb' || value.transport === 'uart') &&
    (value.state === 'available' || value.state === 'unavailable') &&
    isGeneration(value.generation)
  )
}

function isFunction(value: unknown): value is ModemFunctionDetail {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'physicalDeviceId', 'displayName', 'backend', 'generation', 'capabilities'])) return false
  return (
    isID(value.id) &&
    isID(value.physicalDeviceId) &&
    isLabel(value.displayName) &&
    ['simulated', 'direct-at', 'direct-qmi', 'direct-mbim', 'modemmanager', 'pcsc'].includes(String(value.backend)) &&
    isGeneration(value.generation) &&
    isHardwareCapabilities(value.capabilities)
  )
}

function isSlot(value: unknown): value is SIMSlotDetail {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'physicalDeviceId', 'index', 'presence', 'activeMediaId', 'generation'])) return false
  return (
    isID(value.id) &&
    isID(value.physicalDeviceId) &&
    Number.isInteger(value.index) &&
    (value.index as number) >= 0 &&
    (value.index as number) <= 255 &&
    (value.presence === 'present' || value.presence === 'absent' || value.presence === 'unknown') &&
    typeof value.activeMediaId === 'string' &&
    (value.activeMediaId === '' || isID(value.activeMediaId)) &&
    isGeneration(value.generation)
  )
}

function isMedia(value: unknown): value is SIMMediaDetail {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'simSlotId', 'kind', 'identityState', 'displayIdentityHint', 'generation'])) return false
  return (
    isID(value.id) &&
    isID(value.simSlotId) &&
    (value.kind === 'uicc' || value.kind === 'removable-euicc') &&
    (value.identityState === 'known' || value.identityState === 'unknown') &&
    isLabel(value.displayIdentityHint, 64) &&
    isGeneration(value.generation)
  )
}

function isProfile(value: unknown): value is SubscriptionProfileDetail {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'simMediaId', 'displayName', 'state', 'displayIdentityHint', 'generation'])) return false
  return (
    isID(value.id) &&
    isID(value.simMediaId) &&
    isLabel(value.displayName) &&
    (value.state === 'active' || value.state === 'inactive' || value.state === 'locked') &&
    isLabel(value.displayIdentityHint, 64) &&
    isGeneration(value.generation)
  )
}

function isGroup(value: unknown): value is ResourceGroupDetail {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'physicalDeviceId', 'displayName', 'resources', 'modemFunctionIds', 'simSlotIds', 'maxActiveCalls', 'maxConcurrentOps', 'generation'])) return false
  if (
    !isID(value.id) ||
    !isID(value.physicalDeviceId) ||
    !isLabel(value.displayName) ||
    !Array.isArray(value.resources) ||
    value.resources.length === 0 ||
    !value.resources.every((item) => typeof item === 'string' && resourceKinds.has(item)) ||
    new Set(value.resources).size !== value.resources.length ||
    !Array.isArray(value.modemFunctionIds) ||
    value.modemFunctionIds.length === 0 ||
    !value.modemFunctionIds.every(isID) ||
    new Set(value.modemFunctionIds).size !== value.modemFunctionIds.length ||
    !Array.isArray(value.simSlotIds) ||
    value.simSlotIds.length === 0 ||
    !value.simSlotIds.every(isID) ||
    new Set(value.simSlotIds).size !== value.simSlotIds.length ||
    !Number.isInteger(value.maxActiveCalls) ||
    (value.maxActiveCalls as number) < 0 ||
    (value.maxActiveCalls as number) > 64 ||
    !Number.isInteger(value.maxConcurrentOps) ||
    (value.maxConcurrentOps as number) < 1 ||
    (value.maxConcurrentOps as number) > 64 ||
    !isGeneration(value.generation)
  ) return false
  return true
}

function isLine(value: unknown): value is HardwareLineDetail {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'physicalDeviceId', 'modemFunctionId', 'subscriptionProfileId', 'resourceGroupId', 'displayName', 'generation', 'capabilities', 'state'])) return false
  return (
    isID(value.id) &&
    isID(value.physicalDeviceId) &&
    isID(value.modemFunctionId) &&
    isID(value.subscriptionProfileId) &&
    isID(value.resourceGroupId) &&
    isLabel(value.displayName) &&
    isGeneration(value.generation) &&
    isHardwareCapabilities(value.capabilities) &&
    (value.state === 'ready' || value.state === 'unavailable')
  )
}

function uniqueByID(values: Array<{ id: string }>): boolean {
  return new Set(values.map((value) => value.id)).size === values.length
}

export function isHardwareTopologyResponse(value: unknown): value is HardwareTopologyResponse {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, ['generation', 'revision', 'observedAt', 'devices', 'modemFunctions', 'simSlots', 'simMedia', 'subscriptionProfiles', 'resourceGroups', 'lines']) ||
    !isGeneration(value.generation) ||
    typeof value.revision !== 'string' ||
    !/^[0-9a-f]{64}$/.test(value.revision) ||
    typeof value.observedAt !== 'string' ||
    Number.isNaN(Date.parse(value.observedAt))
  ) {
    return false
  }
  const arrays = [
    value.devices,
    value.modemFunctions,
    value.simSlots,
    value.simMedia,
    value.subscriptionProfiles,
    value.resourceGroups,
    value.lines,
  ]
  if (!arrays.every((items) => Array.isArray(items) && items.length <= 1024)) return false
  if (
    !(value.devices as unknown[]).every(isDevice) ||
    !(value.modemFunctions as unknown[]).every(isFunction) ||
    !(value.simSlots as unknown[]).every(isSlot) ||
    !(value.simMedia as unknown[]).every(isMedia) ||
    !(value.subscriptionProfiles as unknown[]).every(isProfile) ||
    !(value.resourceGroups as unknown[]).every(isGroup) ||
    !(value.lines as unknown[]).every(isLine)
  ) return false

  const topology = value as unknown as HardwareTopologyResponse
  const generations = [
    ...topology.devices,
    ...topology.modemFunctions,
    ...topology.simSlots,
    ...topology.simMedia,
    ...topology.subscriptionProfiles,
    ...topology.resourceGroups,
    ...topology.lines,
  ]
  if (generations.some((item) => item.generation > topology.generation)) return false
  if (
    !uniqueByID(topology.devices) ||
    !uniqueByID(topology.modemFunctions) ||
    !uniqueByID(topology.simSlots) ||
    !uniqueByID(topology.simMedia) ||
    !uniqueByID(topology.subscriptionProfiles) ||
    !uniqueByID(topology.resourceGroups) ||
    !uniqueByID(topology.lines)
  ) return false

  const devices = new Map(topology.devices.map((device) => [device.id, device]))
  const functions = new Map(topology.modemFunctions.map((item) => [item.id, item]))
  const slots = new Map(topology.simSlots.map((slot) => [slot.id, slot]))
  const media = new Map(topology.simMedia.map((item) => [item.id, item]))
  const profiles = new Map(topology.subscriptionProfiles.map((profile) => [profile.id, profile]))
  const groups = new Map(topology.resourceGroups.map((group) => [group.id, group]))

  if (!topology.modemFunctions.every((item) => devices.has(item.physicalDeviceId))) return false
  const slotIndexes = new Set<string>()
  for (const slot of topology.simSlots) {
    if (!devices.has(slot.physicalDeviceId)) return false
    const indexKey = `${slot.physicalDeviceId}/${slot.index}`
    if (slotIndexes.has(indexKey)) return false
    slotIndexes.add(indexKey)
    if (slot.activeMediaId !== '') {
      const item = media.get(slot.activeMediaId)
      if (!item || item.simSlotId !== slot.id || slot.presence !== 'present') return false
    }
  }
  const occupiedSlots = new Set<string>()
  for (const item of topology.simMedia) {
    const slot = slots.get(item.simSlotId)
    if (!slot || occupiedSlots.has(item.simSlotId) || slot.presence !== 'present' || slot.activeMediaId !== item.id) return false
    occupiedSlots.add(item.simSlotId)
  }
  const enabledProfileMedia = new Set<string>()
  for (const profile of topology.subscriptionProfiles) {
    if (!media.has(profile.simMediaId)) return false
    if (profile.state === 'active' || profile.state === 'locked') {
      if (enabledProfileMedia.has(profile.simMediaId)) return false
      enabledProfileMedia.add(profile.simMediaId)
    }
  }
  for (const group of topology.resourceGroups) {
    const groupDevice = devices.get(group.physicalDeviceId)
    if (!groupDevice || groupDevice.generation > group.generation || (group.maxActiveCalls > 0 && !group.resources.includes('voice-media') && !group.resources.includes('host-vowifi-auth'))) return false
    const boundFunctions: ModemFunctionDetail[] = []
    for (const functionID of group.modemFunctionIds) {
      const item = functions.get(functionID)
      if (!item || item.physicalDeviceId !== group.physicalDeviceId || item.generation > group.generation) return false
      boundFunctions.push(item)
    }
    if (group.resources.some((resource) => !resourceSupported(resource, boundFunctions))) return false
    for (const slotID of group.simSlotIds) {
      const slot = slots.get(slotID)
      if (!slot || slot.physicalDeviceId !== group.physicalDeviceId || slot.generation > group.generation) return false
    }
  }
  const lineProfiles = new Set<string>()
  for (const line of topology.lines) {
    const fn = functions.get(line.modemFunctionId)
    const profile = profiles.get(line.subscriptionProfileId)
    const group = groups.get(line.resourceGroupId)
    const item = profile ? media.get(profile.simMediaId) : undefined
    const slot = item ? slots.get(item.simSlotId) : undefined
    const device = devices.get(line.physicalDeviceId)
    const expectedState = device?.state === 'unavailable' || profile?.state === 'locked' ? 'unavailable' : 'ready'
    if (
      !device ||
      !fn ||
      !line.capabilities.simAccess ||
      fn.generation > line.generation ||
      (profile !== undefined && profile.generation > line.generation) ||
      (profile !== undefined && group !== undefined && profile.generation > group.generation) ||
      (group !== undefined && group.generation > line.generation) ||
      line.state !== expectedState ||
      fn.physicalDeviceId !== line.physicalDeviceId ||
      !capabilitiesSubset(line.capabilities, fn.capabilities) ||
      (line.capabilities.cellularVoice && !line.capabilities.digitalVoiceMedia) ||
      !profile ||
      (profile.state !== 'active' && profile.state !== 'locked') ||
      !group ||
      group.physicalDeviceId !== line.physicalDeviceId ||
      !group.modemFunctionIds.includes(line.modemFunctionId) ||
      !group.resources.includes('sim-access') ||
      (line.capabilities.rfControl && !group.resources.includes('radio-control')) ||
      (line.capabilities.sms && !group.resources.includes('sms-storage')) ||
      ((line.capabilities.cellularVoice || line.capabilities.digitalVoiceMedia) && !group.resources.includes('voice-media')) ||
      (line.capabilities.simApdu && !group.resources.includes('sim-apdu')) ||
      (line.capabilities.hostVoWifiAuth && !group.resources.includes('host-vowifi-auth')) ||
      ((line.capabilities.networkScan || line.capabilities.manualNetworkSelection) && !group.resources.includes('network-selection')) ||
      ((line.capabilities.primarySimLockState || line.capabilities.pin1Verify || line.capabilities.puk1Unblock) && !group.resources.includes('sim-lock')) ||
      (line.capabilities.euiccProfiles && !group.resources.includes('euicc-profiles')) ||
      !item ||
      !slot ||
      slot.physicalDeviceId !== line.physicalDeviceId ||
      slot.activeMediaId !== item.id ||
      item.generation > group.generation ||
      slot.generation > group.generation ||
      item.generation > line.generation ||
      slot.generation > line.generation ||
      !group.simSlotIds.includes(slot.id) ||
      lineProfiles.has(line.subscriptionProfileId)
    ) return false
    lineProfiles.add(line.subscriptionProfileId)
  }
  return true
}
