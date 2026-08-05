import { isHardwareTopologyResponse, type HardwareTopologyResponse } from './hardwareSchema'
import type { components } from './schema'

export type HealthResponse = components['schemas']['HealthResponse']
export type SetupStatusResponse = components['schemas']['SetupStatusResponse']
export type AuthSessionResponse = components['schemas']['AuthSessionResponse']
export type LoginRequest = components['schemas']['LoginRequest']
export type ChangeAdministratorPasswordRequest = components['schemas']['ChangeAdministratorPasswordRequest']
export type MihomoCoreStatus = components['schemas']['MihomoCoreStatus']
export type MihomoCoreCandidate = components['schemas']['MihomoCoreCandidate']
export type MihomoSubscription = components['schemas']['MihomoSubscription']
export type MihomoSubscriptionCreateRequest = components['schemas']['MihomoSubscriptionCreateRequest']
export type MihomoSubscriptionMutation = components['schemas']['MihomoSubscriptionMutation']
export type MihomoNode = components['schemas']['MihomoNode']
export type MihomoSubscriptionRefresh = components['schemas']['MihomoSubscriptionRefresh']
export type MihomoEgressProfile = components['schemas']['MihomoEgressProfile']
export type MihomoEgressProfileMutation = components['schemas']['MihomoEgressProfileMutation']
export type LineEgressBinding = components['schemas']['LineEgressBinding']
export type LineEgressBindingMutation = components['schemas']['LineEgressBindingMutation']
export type VoWiFiLineState = components['schemas']['VoWiFiLineState']
export type MihomoConfigStatus = components['schemas']['MihomoConfigStatus']
export type MihomoRuntimeStatus = components['schemas']['MihomoRuntimeStatus']
export type NotificationChannel = components['schemas']['NotificationChannel']
export type NotificationChannelMutation = components['schemas']['NotificationChannelMutation']
export type NotificationEventKind = components['schemas']['NotificationEventKind']
export type SetupSessionResponse = components['schemas']['SetupSessionResponse']
export type ConfigureSetupAdministratorRequest = components['schemas']['ConfigureSetupAdministratorRequest']
export type ConfigureSetupStorageRequest = components['schemas']['ConfigureSetupStorageRequest']
export type ConfigureSetupHTTPSRequest = components['schemas']['ConfigureSetupHTTPSRequest']
export type SetupRootCertificateResponse = components['schemas']['SetupRootCertificateResponse']
export type SetupCompletionResponse = components['schemas']['SetupCompletionResponse']
export type SetupFlow = components['schemas']['SetupFlow']
export type InventoryResponse = components['schemas']['InventoryResponse']
export type AccessMode = components['schemas']['AccessMode']
export type LineSummary = components['schemas']['LineSummary']
export type SMSMessage = components['schemas']['SMSMessage']
export type SMSMessageListResponse = components['schemas']['SMSMessageListResponse']
export type SendSMSRequest = components['schemas']['SendSMSRequest']
export type Contact = components['schemas']['Contact']
export type ContactMutationRequest = components['schemas']['ContactMutationRequest']
export type ContactListResponse = components['schemas']['ContactListResponse']
export type Call = components['schemas']['Call']
export type CallListResponse = components['schemas']['CallListResponse']
export type CallStartRequest = components['schemas']['CallStartRequest']
export type CallActionRequest = components['schemas']['CallActionRequest']
export type EUICCState = components['schemas']['EUICCState']
export type AccessPathState = components['schemas']['AccessPathState']
export type AccessPathRequest = components['schemas']['AccessPathRequest']
export type AccessPathListResponse = components['schemas']['AccessPathListResponse']
export type { HardwareTopologyResponse }
type ApiError = components['schemas']['ApiError']
type PhysicalDeviceSummary = components['schemas']['PhysicalDeviceSummary']

const idPattern = /^[a-z0-9][a-z0-9-]{0,63}$/
const bootstrapCodePattern = /^[A-Za-z0-9_-]{43}$/
const operationIdPattern = /^[A-Za-z0-9_-]{16,128}$/
const destinationPattern = /^\+?[0-9]{3,20}$/
const remoteAddressPattern = /^(?:\+?[0-9]{3,20}|[A-Za-z][A-Za-z0-9 ._-]{0,19})$/
const contactIdPattern = /^contact_[A-Za-z0-9_-]{16,120}$/
const callIdPattern = /^call_[A-Za-z0-9_-]{16,120}$/

function isApiError(value: unknown): value is ApiError {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<ApiError>
  return typeof candidate.code === 'string' && /^[A-Z0-9_]+$/.test(candidate.code) && typeof candidate.retryable === 'boolean'
}

export function isAccessMode(value: unknown): value is AccessMode {
  return value === 'cellular-native' || value === 'host-vowifi-only' || value === 'hold-rf-off'
}

function cookieValue(name: string): string {
  if (typeof document === 'undefined') return ''
  const prefix = `${encodeURIComponent(name)}=`
  for (const value of document.cookie.split(';')) {
    const trimmed = value.trim()
    if (trimmed.startsWith(prefix)) return decodeURIComponent(trimmed.slice(prefix.length))
  }
  return ''
}

async function requestJSON(
  path: string,
  signal: AbortSignal | undefined,
  networkCode: string,
  invalidCode: string,
  httpCodePrefix: string,
  init: RequestInit = {},
): Promise<unknown> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  const method = init.method?.toUpperCase() ?? 'GET'
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method) && !path.startsWith('/api/v1/setup/') && path !== '/api/v1/auth/login') {
    const csrf = cookieValue('simplus_csrf')
    if (csrf) headers.set('X-Simplus-CSRF', csrf)
  }

  let response: Response
  try {
    response = await fetch(path, {
      ...init,
      headers,
      signal,
    })
  } catch (error) {
    if (error instanceof Error && error.name === 'AbortError') throw error
    throw new Error(networkCode)
  }
  if (!response.ok) {
    try {
      const error = (await response.json()) as unknown
      if (isApiError(error)) throw new Error(error.code)
    } catch (error) {
      if (error instanceof Error && /^[A-Z0-9_]+$/.test(error.message)) throw error
    }
    throw new Error(`${httpCodePrefix}_HTTP_${response.status}`)
  }
  if (response.status === 204) return null
  try {
    return (await response.json()) as unknown
  } catch {
    throw new Error(invalidCode)
  }
}

function isSetupStatusResponse(value: unknown): value is SetupStatusResponse {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<SetupStatusResponse>
  if (
    typeof candidate.setupRequired !== 'boolean' ||
    typeof candidate.businessApiAvailable !== 'boolean' ||
    typeof candidate.bootstrapGenerationAvailable !== 'boolean' ||
    !Array.isArray(candidate.supportedFlows) ||
    candidate.supportedFlows.length > 1 ||
    !candidate.supportedFlows.every((flow) => flow === 'create-new')
  ) {
    return false
  }

  if (candidate.installationState === 'uninitialized') {
    return (
      candidate.phase === 'bootstrap-required' &&
      candidate.setupRequired &&
      !candidate.businessApiAvailable &&
      candidate.supportedFlows.length === 1 &&
      candidate.supportedFlows[0] === 'create-new'
    )
  }
  if (candidate.installationState === 'ready') {
    return (
      candidate.phase === 'complete' &&
      !candidate.setupRequired &&
      candidate.businessApiAvailable &&
      !candidate.bootstrapGenerationAvailable &&
      candidate.supportedFlows.length === 0
    )
  }
  return (
    candidate.installationState === 'maintenance' &&
    candidate.phase === 'maintenance' &&
    !candidate.setupRequired &&
    !candidate.businessApiAvailable &&
    !candidate.bootstrapGenerationAvailable &&
    candidate.supportedFlows.length === 0
  )
}

function isSetupSessionResponse(value: unknown): value is SetupSessionResponse {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<SetupSessionResponse>
  const httpsIsValid = (() => {
    if (
      typeof candidate.httpsConfigured !== 'boolean' ||
      typeof candidate.httpsConfirmed !== 'boolean' ||
      typeof candidate.httpsMode !== 'string' ||
      typeof candidate.httpsListenUrl !== 'string' ||
      typeof candidate.httpsRootFingerprint !== 'string' ||
      typeof candidate.httpsLeafNotAfter !== 'string'
    ) return false
    if (!candidate.httpsConfigured) {
      return !candidate.httpsConfirmed && candidate.httpsMode === '' && candidate.httpsListenUrl === '' && candidate.httpsRootFingerprint === '' && candidate.httpsLeafNotAfter === ''
    }
    if (candidate.httpsMode === 'loopback-only') {
      return candidate.httpsConfirmed && candidate.httpsListenUrl.startsWith('http://') && candidate.httpsRootFingerprint === '' && candidate.httpsLeafNotAfter === ''
    }
    if (candidate.httpsMode === 'local-ca') {
      return (
        candidate.httpsListenUrl.startsWith('https://') &&
        /^[0-9A-F]{2}(:[0-9A-F]{2}){31}$/.test(candidate.httpsRootFingerprint) &&
        !Number.isNaN(Date.parse(candidate.httpsLeafNotAfter))
      )
    }
    return false
  })()
  return (
    candidate.authorized === true &&
    typeof candidate.expiresAt === 'string' &&
    !Number.isNaN(Date.parse(candidate.expiresAt)) &&
    candidate.selectedFlow === 'create-new' &&
    Array.isArray(candidate.supportedFlows) &&
    candidate.supportedFlows.length === 1 &&
    candidate.supportedFlows[0] === 'create-new' &&
    typeof candidate.administratorConfigured === 'boolean' &&
    typeof candidate.administratorUsername === 'string' &&
    /^[a-z0-9._-]{0,32}$/.test(candidate.administratorUsername) &&
    (candidate.administratorConfigured ? candidate.administratorUsername.length >= 3 : candidate.administratorUsername.length === 0) &&
    (candidate.instanceDefaultLocale === 'zh-CN' || candidate.instanceDefaultLocale === 'en-US') &&
    typeof candidate.storageConfigured === 'boolean' &&
    typeof candidate.dataRoot === 'string' &&
    candidate.dataRoot.startsWith('/') &&
    typeof candidate.recordingsRoot === 'string' &&
    candidate.recordingsRoot.startsWith('/') &&
    httpsIsValid &&
    typeof candidate.hardwareReviewed === 'boolean' &&
    Number.isInteger(candidate.hardwareDeviceCount) &&
    (candidate.hardwareDeviceCount ?? -1) >= 0 &&
    Number.isInteger(candidate.hardwareLineCount) &&
    (candidate.hardwareLineCount ?? -1) >= 0 &&
    typeof candidate.hardwareInventoryDigest === 'string' &&
    (candidate.hardwareReviewed
      ? candidate.httpsConfigured === true && candidate.httpsConfirmed === true && /^[0-9a-f]{64}$/.test(candidate.hardwareInventoryDigest)
      : candidate.hardwareInventoryDigest === '')
  )
}

function isHealthResponse(value: unknown): value is HealthResponse {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<HealthResponse>
  return (
    (candidate.status === 'ok' || candidate.status === 'degraded') &&
    typeof candidate.version === 'string' &&
    candidate.apiVersion === 'v1' &&
    (candidate.installationState === 'uninitialized' ||
      candidate.installationState === 'ready' ||
      candidate.installationState === 'maintenance') &&
    (candidate.rfSafety === 'off' || candidate.rfSafety === 'not-present' || candidate.rfSafety === 'unknown') &&
    (candidate.backend === 'simulator' || candidate.backend === 'hardware' || candidate.backend === 'replay') &&
    Number.isInteger(candidate.databaseCount) &&
    (candidate.databaseCount ?? -1) >= 0
  )
}

function isPhysicalDevice(value: unknown): value is PhysicalDeviceSummary {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<PhysicalDeviceSummary>
  return (
    typeof candidate.id === 'string' &&
    idPattern.test(candidate.id) &&
    typeof candidate.displayName === 'string' &&
    candidate.displayName.length > 0 &&
    (candidate.transport === 'simulated' || candidate.transport === 'usb' || candidate.transport === 'uart') &&
    (candidate.state === 'available' || candidate.state === 'unavailable') &&
    Number.isSafeInteger(candidate.generation) &&
    (candidate.generation ?? 0) > 0 &&
    Number.isInteger(candidate.modemFunctionCount) &&
    (candidate.modemFunctionCount ?? -1) >= 0 &&
    Number.isInteger(candidate.simSlotCount) &&
    (candidate.simSlotCount ?? -1) >= 0 &&
    Number.isInteger(candidate.resourceGroupCount) &&
    (candidate.resourceGroupCount ?? -1) >= 0
  )
}

function isLine(value: unknown): value is LineSummary {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<LineSummary>
  const identityIsValid =
    typeof candidate.id === 'string' &&
    idPattern.test(candidate.id) &&
    typeof candidate.physicalDeviceId === 'string' &&
    idPattern.test(candidate.physicalDeviceId) &&
    typeof candidate.subscriptionProfileId === 'string' &&
    idPattern.test(candidate.subscriptionProfileId)
  const stateIsValid =
    candidate.state === 'awaiting-access-mode' || candidate.state === 'ready' || candidate.state === 'unavailable'
  const rfIsValid = candidate.rfSafety === 'off' || candidate.rfSafety === 'not-present' || candidate.rfSafety === 'unknown'
  const configurationIsConsistent =
    typeof candidate.accessModeConfigured === 'boolean' &&
    (candidate.accessModeConfigured
      ? candidate.state !== 'awaiting-access-mode'
      : candidate.accessMode === 'hold-rf-off' && candidate.state === 'awaiting-access-mode')

  return (
    identityIsValid &&
    typeof candidate.displayName === 'string' &&
    candidate.displayName.length > 0 &&
    Number.isSafeInteger(candidate.generation) &&
    (candidate.generation ?? 0) > 0 &&
    isAccessMode(candidate.accessMode) &&
    stateIsValid &&
    rfIsValid &&
    configurationIsConsistent
  )
}

function isInventoryResponse(value: unknown): value is InventoryResponse {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<InventoryResponse>
  if (!Number.isSafeInteger(candidate.generation) || (candidate.generation ?? 0) <= 0) return false
  if (typeof candidate.revision !== 'string' || !/^[0-9a-f]{64}$/.test(candidate.revision)) return false
  if (typeof candidate.observedAt !== 'string' || Number.isNaN(Date.parse(candidate.observedAt))) return false
  if (!Array.isArray(candidate.devices) || !candidate.devices.every(isPhysicalDevice)) return false
  if (!Array.isArray(candidate.lines) || !candidate.lines.every(isLine)) return false

  const deviceIds = new Set(candidate.devices.map((device) => device.id))
  const lineIds = new Set(candidate.lines.map((line) => line.id))
  const profileIds = new Set(candidate.lines.map((line) => line.subscriptionProfileId))
  return (
    deviceIds.size === candidate.devices.length &&
    lineIds.size === candidate.lines.length &&
    profileIds.size === candidate.lines.length &&
    candidate.lines.every((line) => deviceIds.has(line.physicalDeviceId))
  )
}

function isSMSMessage(value: unknown): value is SMSMessage {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<SMSMessage>
  if (
    typeof candidate.id !== 'string' || !operationIdPattern.test(candidate.id) ||
    typeof candidate.operationId !== 'string' || !operationIdPattern.test(candidate.operationId) ||
    typeof candidate.lineId !== 'string' || !idPattern.test(candidate.lineId) ||
    typeof candidate.remoteAddress !== 'string' || !remoteAddressPattern.test(candidate.remoteAddress) ||
    typeof candidate.body !== 'string' || candidate.body.trim().length === 0 || [...candidate.body].length > 1600 ||
    typeof candidate.providerMessageId !== 'string' || candidate.providerMessageId.length > 128 ||
    typeof candidate.errorCode !== 'string' || candidate.errorCode.length > 64 ||
    typeof candidate.createdAt !== 'string' || !Number.isFinite(Date.parse(candidate.createdAt)) ||
    typeof candidate.updatedAt !== 'string' || !Number.isFinite(Date.parse(candidate.updatedAt)) ||
    Date.parse(candidate.updatedAt) < Date.parse(candidate.createdAt)
  ) return false

  const hasSentAt = typeof candidate.sentAt === 'string' && Number.isFinite(Date.parse(candidate.sentAt))
  switch (candidate.status) {
    case 'queued':
      return candidate.direction === 'outbound' && candidate.providerMessageId === '' && candidate.errorCode === '' && candidate.sentAt === undefined
    case 'sent':
      return candidate.direction === 'outbound' && candidate.providerMessageId.length > 0 && candidate.errorCode === '' && hasSentAt
    case 'unconfirmed':
      return candidate.direction === 'outbound' && /^[A-Z][A-Z0-9_]{0,63}$/.test(candidate.errorCode) && candidate.sentAt === undefined
    case 'failed':
      return candidate.direction === 'outbound' && /^[A-Z][A-Z0-9_]{0,63}$/.test(candidate.errorCode) && candidate.sentAt === undefined
    case 'received':
      return candidate.direction === 'inbound' && candidate.providerMessageId.length > 0 && candidate.errorCode === '' && candidate.sentAt === undefined
    default:
      return false
  }
}

function isContact(value: unknown): value is Contact {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<Contact>
  return (
    typeof candidate.id === 'string' && contactIdPattern.test(candidate.id) &&
    typeof candidate.displayName === 'string' && candidate.displayName.trim().length > 0 && [...candidate.displayName].length <= 80 &&
    typeof candidate.phoneNumber === 'string' && destinationPattern.test(candidate.phoneNumber) &&
    typeof candidate.createdAt === 'string' && Number.isFinite(Date.parse(candidate.createdAt)) &&
    typeof candidate.updatedAt === 'string' && Number.isFinite(Date.parse(candidate.updatedAt)) &&
    Date.parse(candidate.updatedAt) >= Date.parse(candidate.createdAt)
  )
}

function isCall(value: unknown): value is Call {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<Call>
  return typeof candidate.id === 'string' && callIdPattern.test(candidate.id) &&
    typeof candidate.operationId === 'string' && operationIdPattern.test(candidate.operationId) &&
    typeof candidate.lineId === 'string' && idPattern.test(candidate.lineId) &&
    typeof candidate.remoteAddress === 'string' && destinationPattern.test(candidate.remoteAddress) &&
    (candidate.direction === 'inbound' || candidate.direction === 'outbound') &&
    ['incoming', 'dialing', 'active', 'ended', 'failed'].includes(candidate.state ?? '') &&
    typeof candidate.endReason === 'string' && candidate.endReason.length <= 64 &&
    typeof candidate.createdAt === 'string' && Number.isFinite(Date.parse(candidate.createdAt)) &&
    typeof candidate.updatedAt === 'string' && Number.isFinite(Date.parse(candidate.updatedAt))
}

export async function getSetupStatus(signal?: AbortSignal): Promise<SetupStatusResponse> {
  const setup = await requestJSON(
    '/api/v1/setup/status',
    signal,
    'SETUP_NETWORK_UNAVAILABLE',
    'SETUP_RESPONSE_INVALID',
    'SETUP',
  )
  if (!isSetupStatusResponse(setup)) throw new Error('SETUP_RESPONSE_INVALID')
  return setup
}

export async function consumeSetupBootstrap(
  bootstrapCode: string,
  signal?: AbortSignal,
): Promise<SetupSessionResponse> {
  if (!bootstrapCodePattern.test(bootstrapCode)) throw new Error('BOOTSTRAP_REQUEST_INVALID')
  const session = await requestJSON(
    '/api/v1/setup/bootstrap',
    signal,
    'SETUP_NETWORK_UNAVAILABLE',
    'SETUP_SESSION_RESPONSE_INVALID',
    'SETUP_BOOTSTRAP',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ bootstrapCode }),
    },
  )
  if (!isSetupSessionResponse(session)) throw new Error('SETUP_SESSION_RESPONSE_INVALID')
  return session
}

export async function getSetupSession(signal?: AbortSignal): Promise<SetupSessionResponse | null> {
  try {
    const session = await requestJSON(
      '/api/v1/setup/session',
      signal,
      'SETUP_NETWORK_UNAVAILABLE',
      'SETUP_SESSION_RESPONSE_INVALID',
      'SETUP_SESSION',
    )
    if (!isSetupSessionResponse(session)) throw new Error('SETUP_SESSION_RESPONSE_INVALID')
    return session
  } catch (error) {
    if (error instanceof Error && error.message === 'SETUP_SESSION_UNAUTHORIZED') return null
    throw error
  }
}

export async function putSetupAdministrator(
  request: ConfigureSetupAdministratorRequest,
  signal?: AbortSignal,
): Promise<SetupSessionResponse> {
  const session = await requestJSON(
    '/api/v1/setup/administrator',
    signal,
    'SETUP_NETWORK_UNAVAILABLE',
    'SETUP_SESSION_RESPONSE_INVALID',
    'SETUP_ADMINISTRATOR',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
    },
  )
  if (!isSetupSessionResponse(session)) throw new Error('SETUP_SESSION_RESPONSE_INVALID')
  return session
}

export async function putSetupStorage(
  request: ConfigureSetupStorageRequest,
  signal?: AbortSignal,
): Promise<SetupSessionResponse> {
  const session = await requestJSON(
    '/api/v1/setup/storage',
    signal,
    'SETUP_NETWORK_UNAVAILABLE',
    'SETUP_SESSION_RESPONSE_INVALID',
    'SETUP_STORAGE',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
    },
  )
  if (!isSetupSessionResponse(session)) throw new Error('SETUP_SESSION_RESPONSE_INVALID')
  return session
}

export async function putSetupHTTPS(
  request: ConfigureSetupHTTPSRequest,
  signal?: AbortSignal,
): Promise<SetupSessionResponse> {
  const session = await requestJSON(
    '/api/v1/setup/https',
    signal,
    'SETUP_NETWORK_UNAVAILABLE',
    'SETUP_SESSION_RESPONSE_INVALID',
    'SETUP_HTTPS',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
    },
  )
  if (!isSetupSessionResponse(session)) throw new Error('SETUP_SESSION_RESPONSE_INVALID')
  return session
}

export async function confirmSetupHTTPS(
  rootFingerprintSha256: string,
  signal?: AbortSignal,
): Promise<SetupSessionResponse> {
  const session = await requestJSON(
    '/api/v1/setup/https/confirm',
    signal,
    'SETUP_NETWORK_UNAVAILABLE',
    'SETUP_SESSION_RESPONSE_INVALID',
    'SETUP_HTTPS_CONFIRMATION',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ rootFingerprintSha256 }),
    },
  )
  if (!isSetupSessionResponse(session)) throw new Error('SETUP_SESSION_RESPONSE_INVALID')
  return session
}

export async function getSetupHTTPSRootCertificate(signal?: AbortSignal): Promise<SetupRootCertificateResponse> {
  const response = await requestJSON(
    '/api/v1/setup/https/root-certificate',
    signal,
    'SETUP_NETWORK_UNAVAILABLE',
    'SETUP_ROOT_CERTIFICATE_RESPONSE_INVALID',
    'SETUP_ROOT_CERTIFICATE',
  )
  if (typeof response !== 'object' || response === null) throw new Error('SETUP_ROOT_CERTIFICATE_RESPONSE_INVALID')
  const candidate = response as Partial<SetupRootCertificateResponse>
  if (
    typeof candidate.pem !== 'string' ||
    !candidate.pem.startsWith('-----BEGIN CERTIFICATE-----') ||
    typeof candidate.rootFingerprintSha256 !== 'string' ||
    !/^[0-9A-F]{2}(:[0-9A-F]{2}){31}$/.test(candidate.rootFingerprintSha256)
  ) throw new Error('SETUP_ROOT_CERTIFICATE_RESPONSE_INVALID')
  return candidate as SetupRootCertificateResponse
}

export async function completeSetup(signal?: AbortSignal): Promise<SetupCompletionResponse> {
  const response = await requestJSON(
    '/api/v1/setup/complete',
    signal,
    'SETUP_NETWORK_UNAVAILABLE',
    'SETUP_COMPLETION_RESPONSE_INVALID',
    'SETUP_COMPLETION',
    { method: 'POST' },
  )
  if (typeof response !== 'object' || response === null) throw new Error('SETUP_COMPLETION_RESPONSE_INVALID')
  const candidate = response as Partial<SetupCompletionResponse>
  if (
    candidate.installationState !== 'ready' ||
    candidate.loginRequired !== true ||
    typeof candidate.managementUrl !== 'string' ||
    !/^https?:\/\//.test(candidate.managementUrl)
  ) throw new Error('SETUP_COMPLETION_RESPONSE_INVALID')
  return candidate as SetupCompletionResponse
}

function validateAuthSession(value: unknown): AuthSessionResponse {
  if (typeof value !== 'object' || value === null) throw new Error('AUTH_SESSION_RESPONSE_INVALID')
  const candidate = value as Partial<AuthSessionResponse>
  if (
    typeof candidate.username !== 'string' || candidate.username.length === 0 ||
    (candidate.locale !== 'zh-CN' && candidate.locale !== 'en-US') ||
    typeof candidate.expiresAt !== 'string' || !Number.isFinite(Date.parse(candidate.expiresAt))
  ) throw new Error('AUTH_SESSION_RESPONSE_INVALID')
  return candidate as AuthSessionResponse
}

export async function login(request: LoginRequest, signal?: AbortSignal): Promise<AuthSessionResponse> {
  const response = await requestJSON(
    '/api/v1/auth/login',
    signal,
    'AUTH_NETWORK_UNAVAILABLE',
    'AUTH_SESSION_RESPONSE_INVALID',
    'LOGIN',
    { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) },
  )
  return validateAuthSession(response)
}

export async function getAuthSession(signal?: AbortSignal): Promise<AuthSessionResponse> {
  const response = await requestJSON('/api/v1/auth/session', signal, 'AUTH_NETWORK_UNAVAILABLE', 'AUTH_SESSION_RESPONSE_INVALID', 'AUTH_SESSION')
  return validateAuthSession(response)
}

export async function logout(signal?: AbortSignal): Promise<void> {
  await requestJSON('/api/v1/auth/logout', signal, 'AUTH_NETWORK_UNAVAILABLE', 'AUTH_LOGOUT_RESPONSE_INVALID', 'AUTH_LOGOUT', { method: 'POST' })
}

export async function changeAdministratorPassword(request: ChangeAdministratorPasswordRequest, signal?: AbortSignal): Promise<void> {
	if (request.newPassword !== request.newPasswordConfirmation || [...request.newPassword].length < 12 || [...request.newPassword].length > 128) throw new Error('PASSWORD_REQUEST_INVALID')
	await requestJSON('/api/v1/auth/password', signal, 'AUTH_NETWORK_UNAVAILABLE', 'AUTH_PASSWORD_RESPONSE_INVALID', 'AUTH_PASSWORD', {
		method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request),
	})
}

function isMihomoCoreStatus(value: unknown): value is MihomoCoreStatus {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Partial<MihomoCoreStatus>
  return typeof item.installed === 'boolean' && typeof item.version === 'string' &&
    (item.architecture === '' || item.architecture === 'amd64' || item.architecture === 'arm64') &&
    typeof item.sha256 === 'string' && (/^[0-9a-f]{64}$/.test(item.sha256) || item.sha256 === '') &&
    typeof item.installedAt === 'string'
}

function isMihomoCoreCandidate(value: unknown): value is MihomoCoreCandidate {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Partial<MihomoCoreCandidate>
  return typeof item.version === 'string' && /^v[0-9]+\.[0-9]+\.[0-9]+$/.test(item.version) &&
    typeof item.assetName === 'string' && item.assetName.length > 0 &&
    typeof item.sha256 === 'string' && /^[0-9a-f]{64}$/.test(item.sha256) &&
    typeof item.size === 'number' && Number.isSafeInteger(item.size) && item.size > 0 &&
    (item.architecture === 'amd64' || item.architecture === 'arm64')
}

export async function getMihomoCoreStatus(signal?: AbortSignal): Promise<MihomoCoreStatus> {
  const response = await requestJSON('/api/v1/mihomo/core', signal, 'MIHOMO_NETWORK_UNAVAILABLE', 'MIHOMO_CORE_RESPONSE_INVALID', 'MIHOMO_CORE')
  if (!isMihomoCoreStatus(response)) throw new Error('MIHOMO_CORE_RESPONSE_INVALID')
  return response
}

export async function getLatestMihomoCore(signal?: AbortSignal): Promise<MihomoCoreCandidate> {
  const response = await requestJSON('/api/v1/mihomo/core/latest', signal, 'MIHOMO_NETWORK_UNAVAILABLE', 'MIHOMO_RELEASE_RESPONSE_INVALID', 'MIHOMO_RELEASE')
  if (!isMihomoCoreCandidate(response)) throw new Error('MIHOMO_RELEASE_RESPONSE_INVALID')
  return response
}

export async function installLatestMihomoCore(signal?: AbortSignal): Promise<MihomoCoreStatus> {
  const response = await requestJSON('/api/v1/mihomo/core/install', signal, 'MIHOMO_NETWORK_UNAVAILABLE', 'MIHOMO_CORE_RESPONSE_INVALID', 'MIHOMO_INSTALL', { method: 'POST' })
  if (!isMihomoCoreStatus(response)) throw new Error('MIHOMO_CORE_RESPONSE_INVALID')
  return response
}

function isMihomoSubscription(value: unknown): value is MihomoSubscription {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Partial<MihomoSubscription>
  return typeof item.id === 'string' && /^subscription_[A-Za-z0-9_-]{22}$/.test(item.id) && typeof item.displayName === 'string' && item.displayName.length > 0 &&
    typeof item.url === 'string' && item.url.startsWith('https://') && typeof item.urlHint === 'string' && item.urlHint.length > 0 && typeof item.enabled === 'boolean' && typeof item.selected === 'boolean' && typeof item.artifactReady === 'boolean' && typeof item.lastRefreshAt === 'string' &&
    (item.lastRefreshStatus === 'never' || item.lastRefreshStatus === 'success' || item.lastRefreshStatus === 'failed') &&
    typeof item.nodeCount === 'number' && Number.isSafeInteger(item.nodeCount) && typeof item.lastErrorCode === 'string'
}
function isMihomoNode(value: unknown): value is MihomoNode { if(typeof value!=='object'||value===null)return false;const item=value as Partial<MihomoNode>;return typeof item.id==='string'&&/^node_[A-Za-z0-9_-]{22}$/.test(item.id)&&typeof item.displayName==='string'&&item.displayName.length>0&&typeof item.kind==='string'&&item.kind.length>0&&typeof item.countryCode==='string'&&typeof item.countryName==='string' }
export async function listMihomoSubscriptions(signal?:AbortSignal):Promise<MihomoSubscription[]>{const response=await requestJSON('/api/v1/mihomo/subscriptions',signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_SUBSCRIPTIONS_RESPONSE_INVALID','MIHOMO_SUBSCRIPTIONS');if(typeof response!=='object'||response===null)throw new Error('MIHOMO_SUBSCRIPTIONS_RESPONSE_INVALID');const items=(response as {subscriptions?:unknown}).subscriptions;if(!Array.isArray(items)||!items.every(isMihomoSubscription))throw new Error('MIHOMO_SUBSCRIPTIONS_RESPONSE_INVALID');return items}
export async function createMihomoSubscription(request:MihomoSubscriptionCreateRequest,signal?:AbortSignal):Promise<MihomoSubscription>{const response=await requestJSON('/api/v1/mihomo/subscriptions',signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_SUBSCRIPTION_RESPONSE_INVALID','MIHOMO_SUBSCRIPTION_CREATE',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});if(!isMihomoSubscription(response))throw new Error('MIHOMO_SUBSCRIPTION_RESPONSE_INVALID');return response}
export async function updateMihomoSubscription(id:string,request:MihomoSubscriptionMutation,signal?:AbortSignal):Promise<MihomoSubscription>{const response=await requestJSON(`/api/v1/mihomo/subscriptions/${encodeURIComponent(id)}`,signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_SUBSCRIPTION_RESPONSE_INVALID','MIHOMO_SUBSCRIPTION_UPDATE',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});if(!isMihomoSubscription(response))throw new Error('MIHOMO_SUBSCRIPTION_RESPONSE_INVALID');return response}
export async function deleteMihomoSubscription(id:string,signal?:AbortSignal):Promise<void>{await requestJSON(`/api/v1/mihomo/subscriptions/${encodeURIComponent(id)}`,signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_SUBSCRIPTION_RESPONSE_INVALID','MIHOMO_SUBSCRIPTION_DELETE',{method:'DELETE'})}
export async function refreshMihomoSubscription(id:string,signal?:AbortSignal):Promise<MihomoSubscriptionRefresh>{const response=await requestJSON(`/api/v1/mihomo/subscriptions/${encodeURIComponent(id)}/refresh`,signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_SUBSCRIPTION_RESPONSE_INVALID','MIHOMO_SUBSCRIPTION_REFRESH',{method:'POST'});if(typeof response!=='object'||response===null)throw new Error('MIHOMO_SUBSCRIPTION_RESPONSE_INVALID');const item=response as Partial<MihomoSubscriptionRefresh>;if(!isMihomoSubscription(item.subscription)||!Array.isArray(item.nodes)||!item.nodes.every(isMihomoNode))throw new Error('MIHOMO_SUBSCRIPTION_RESPONSE_INVALID');return item as MihomoSubscriptionRefresh}
export async function listMihomoSubscriptionNodes(id:string,signal?:AbortSignal):Promise<MihomoNode[]>{const response=await requestJSON(`/api/v1/mihomo/subscriptions/${encodeURIComponent(id)}/nodes`,signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_NODES_RESPONSE_INVALID','MIHOMO_NODES');if(typeof response!=='object'||response===null)throw new Error('MIHOMO_NODES_RESPONSE_INVALID');const nodes=(response as {nodes?:unknown}).nodes;if(!Array.isArray(nodes)||!nodes.every(isMihomoNode))throw new Error('MIHOMO_NODES_RESPONSE_INVALID');return nodes}
function isMihomoEgressProfile(value:unknown):value is MihomoEgressProfile{if(typeof value!=='object'||value===null)return false;const item=value as Partial<MihomoEgressProfile>;return typeof item.id==='string'&&/^egress_[A-Za-z0-9_-]{22}$/.test(item.id)&&typeof item.displayName==='string'&&typeof item.subscriptionId==='string'&&typeof item.lineId==='string'&&item.lineId.length>0&&(item.selectionType==='node'||item.selectionType==='country')&&typeof item.selectedNodeId==='string'&&typeof item.selectedNodeName==='string'&&typeof item.selectedCountryCode==='string'&&typeof item.selectedCountryName==='string'&&typeof item.sourceCidr==='string'&&typeof item.enabled==='boolean'&&typeof item.ready==='boolean'&&typeof item.readinessReason==='string'}
export async function listMihomoEgressProfiles(signal?:AbortSignal):Promise<MihomoEgressProfile[]>{const response=await requestJSON('/api/v1/mihomo/egress-profiles',signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_EGRESS_RESPONSE_INVALID','MIHOMO_EGRESS');if(typeof response!=='object'||response===null)throw new Error('MIHOMO_EGRESS_RESPONSE_INVALID');const profiles=(response as {profiles?:unknown}).profiles;if(!Array.isArray(profiles)||!profiles.every(isMihomoEgressProfile))throw new Error('MIHOMO_EGRESS_RESPONSE_INVALID');return profiles}
export async function createMihomoEgressProfile(request:MihomoEgressProfileMutation,signal?:AbortSignal):Promise<MihomoEgressProfile>{const response=await requestJSON('/api/v1/mihomo/egress-profiles',signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_EGRESS_RESPONSE_INVALID','MIHOMO_EGRESS_CREATE',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});if(!isMihomoEgressProfile(response))throw new Error('MIHOMO_EGRESS_RESPONSE_INVALID');return response}
export async function deleteMihomoEgressProfile(id:string,signal?:AbortSignal):Promise<void>{await requestJSON(`/api/v1/mihomo/egress-profiles/${encodeURIComponent(id)}`,signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_EGRESS_RESPONSE_INVALID','MIHOMO_EGRESS_DELETE',{method:'DELETE'})}
function isLineEgressBinding(value:unknown):value is LineEgressBinding{if(typeof value!=='object'||value===null)return false;const item=value as Partial<LineEgressBinding>;return typeof item.lineId==='string'&&idPattern.test(item.lineId)&&(item.mode==='direct'||item.mode==='mihomo-country')&&typeof item.countryCode==='string'&&(/^$|^[A-Z]{2}$/).test(item.countryCode)&&typeof item.countryName==='string'&&typeof item.listenerPort==='number'&&Number.isInteger(item.listenerPort)&&item.listenerPort>=0&&item.listenerPort<=65535&&typeof item.ready==='boolean'&&['READY','LINE_NOT_HOST_VOWIFI','SUBSCRIPTION_NOT_SELECTED','COUNTRY_NOT_FOUND','MIHOMO_NOT_RUNNING','MIHOMO_RESTART_REQUIRED'].includes(item.readinessReason??'')}
export async function listLineEgressBindings(signal?:AbortSignal):Promise<LineEgressBinding[]>{const response=await requestJSON('/api/v1/line-egress-bindings',signal,'LINE_EGRESS_NETWORK_UNAVAILABLE','LINE_EGRESS_RESPONSE_INVALID','LINE_EGRESS');if(typeof response!=='object'||response===null)throw new Error('LINE_EGRESS_RESPONSE_INVALID');const bindings=(response as {bindings?:unknown}).bindings;if(!Array.isArray(bindings)||!bindings.every(isLineEgressBinding))throw new Error('LINE_EGRESS_RESPONSE_INVALID');return bindings}
export async function putLineEgressBinding(lineId:string,request:LineEgressBindingMutation,signal?:AbortSignal):Promise<LineEgressBinding>{if(!idPattern.test(lineId)||(request.mode==='direct'&&request.countryCode!=='')||(request.mode==='mihomo-country'&&!/^[A-Z]{2}$/.test(request.countryCode)))throw new Error('LINE_EGRESS_REQUEST_INVALID');const response=await requestJSON(`/api/v1/lines/${encodeURIComponent(lineId)}/egress`,signal,'LINE_EGRESS_NETWORK_UNAVAILABLE','LINE_EGRESS_RESPONSE_INVALID','LINE_EGRESS',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});if(!isLineEgressBinding(response))throw new Error('LINE_EGRESS_RESPONSE_INVALID');return response}
function isVoWiFiLineState(value:unknown):value is VoWiFiLineState{if(typeof value!=='object'||value===null)return false;const item=value as Partial<VoWiFiLineState>;return typeof item.lineId==='string'&&idPattern.test(item.lineId)&&typeof item.desiredActive==='boolean'&&typeof item.eligible==='boolean'&&['READY','LINE_NOT_HOST_VOWIFI','LINE_HARDWARE_NOT_READY','SUBSCRIPTION_NOT_SELECTED','COUNTRY_NOT_FOUND','MIHOMO_NOT_RUNNING','MIHOMO_RESTART_REQUIRED'].includes(item.readinessCode??'')&&['stopped','starting','connecting','registering','online','reconnecting','stopping','failed'].includes(item.state??'')&&typeof item.stage==='string'&&item.stage.length<=64&&typeof item.online==='boolean'&&item.online===(item.state==='online')&&(item.egressMode==='direct'||item.egressMode==='mihomo-country')&&typeof item.countryCode==='string'&&(item.egressMode==='direct'?item.countryCode==='':/^[A-Z]{2}$/.test(item.countryCode))&&typeof item.countryName==='string'&&typeof item.registeredAt==='string'&&typeof item.nextRefreshAt==='string'&&typeof item.attempt==='number'&&Number.isInteger(item.attempt)&&item.attempt>=0&&item.attempt<=1000000&&typeof item.lastErrorCode==='string'&&/^[A-Z0-9_-]*$/.test(item.lastErrorCode)}
export async function listVoWiFiLines(signal?:AbortSignal):Promise<VoWiFiLineState[]>{const response=await requestJSON('/api/v1/vowifi-lines',signal,'VOWIFI_NETWORK_UNAVAILABLE','VOWIFI_RESPONSE_INVALID','VOWIFI');if(typeof response!=='object'||response===null)throw new Error('VOWIFI_RESPONSE_INVALID');const lines=(response as {lines?:unknown}).lines;if(!Array.isArray(lines)||!lines.every(isVoWiFiLineState))throw new Error('VOWIFI_RESPONSE_INVALID');return lines}
async function mutateVoWiFiLine(lineId:string,action:'activate'|'deactivate',signal?:AbortSignal):Promise<VoWiFiLineState>{if(!/^agent-line-[0-9a-f]{32}$/.test(lineId))throw new Error('VOWIFI_LINE_INVALID');const response=await requestJSON(`/api/v1/vowifi-lines/${encodeURIComponent(lineId)}/${action}`,signal,'VOWIFI_NETWORK_UNAVAILABLE','VOWIFI_RESPONSE_INVALID','VOWIFI',{method:'POST'});if(!isVoWiFiLineState(response))throw new Error('VOWIFI_RESPONSE_INVALID');return response}
export const activateVoWiFiLine=(lineId:string,signal?:AbortSignal)=>mutateVoWiFiLine(lineId,'activate',signal)
export const deactivateVoWiFiLine=(lineId:string,signal?:AbortSignal)=>mutateVoWiFiLine(lineId,'deactivate',signal)
function isMihomoConfigStatus(value:unknown):value is MihomoConfigStatus{if(typeof value!=='object'||value===null)return false;const item=value as Partial<MihomoConfigStatus>;return typeof item.published==='boolean'&&typeof item.launchable==='boolean'&&typeof item.sha256==='string'&&typeof item.generatedAt==='string'&&typeof item.errorCode==='string'&&typeof item.selectedSubscriptionId==='string'&&typeof item.runningSubscriptionId==='string'}
export async function getMihomoConfigStatus(signal?:AbortSignal):Promise<MihomoConfigStatus>{const response=await requestJSON('/api/v1/mihomo/config',signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_CONFIG_RESPONSE_INVALID','MIHOMO_CONFIG');if(!isMihomoConfigStatus(response))throw new Error('MIHOMO_CONFIG_RESPONSE_INVALID');return response}
export async function publishMihomoConfig(signal?:AbortSignal):Promise<MihomoConfigStatus>{const response=await requestJSON('/api/v1/mihomo/config',signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_CONFIG_RESPONSE_INVALID','MIHOMO_CONFIG_PUBLISH',{method:'POST'});if(!isMihomoConfigStatus(response))throw new Error('MIHOMO_CONFIG_RESPONSE_INVALID');return response}
export async function selectMihomoSubscription(id:string,signal?:AbortSignal):Promise<MihomoConfigStatus>{const response=await requestJSON(`/api/v1/mihomo/subscriptions/${encodeURIComponent(id)}/select`,signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_CONFIG_RESPONSE_INVALID','MIHOMO_SUBSCRIPTION_SELECT',{method:'POST'});if(!isMihomoConfigStatus(response))throw new Error('MIHOMO_CONFIG_RESPONSE_INVALID');return response}
function isMihomoRuntimeStatus(value:unknown):value is MihomoRuntimeStatus{if(typeof value!=='object'||value===null)return false;const item=value as Partial<MihomoRuntimeStatus>;return (item.state==='stopped'||item.state==='running'||item.state==='fault')&&typeof item.pid==='number'&&typeof item.selectedSubscriptionId==='string'&&typeof item.runningSubscriptionId==='string'&&typeof item.pendingRestart==='boolean'&&typeof item.startedAt==='string'&&typeof item.lastErrorCode==='string'}
async function mihomoRuntime(path:string,method:'GET'|'POST',signal?:AbortSignal):Promise<MihomoRuntimeStatus>{const response=await requestJSON(path,signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_RUNTIME_RESPONSE_INVALID','MIHOMO_RUNTIME',{method});if(!isMihomoRuntimeStatus(response))throw new Error('MIHOMO_RUNTIME_RESPONSE_INVALID');return response}
export const getMihomoRuntimeStatus=(signal?:AbortSignal)=>mihomoRuntime('/api/v1/mihomo/runtime','GET',signal)
export const startMihomo=(signal?:AbortSignal)=>mihomoRuntime('/api/v1/mihomo/runtime/start','POST',signal)
export const restartMihomo=(signal?:AbortSignal)=>mihomoRuntime('/api/v1/mihomo/runtime/restart','POST',signal)
export const stopMihomo=(signal?:AbortSignal)=>mihomoRuntime('/api/v1/mihomo/runtime/stop','POST',signal)
export type MihomoDashboardStatus={available:boolean;version:string;controllerAddress:string;url:string;secret:string}
function isMihomoDashboardStatus(value:unknown):value is MihomoDashboardStatus{if(typeof value!=='object'||value===null)return false;const item=value as Partial<MihomoDashboardStatus>;return typeof item.available==='boolean'&&typeof item.version==='string'&&/^v\d+\.\d+\.\d+$/.test(item.version)&&typeof item.controllerAddress==='string'&&typeof item.url==='string'&&typeof item.secret==='string'&&/^[A-Za-z0-9_-]{43}$/.test(item.secret)}
export async function getMihomoDashboardStatus(signal?:AbortSignal):Promise<MihomoDashboardStatus>{const response=await requestJSON('/api/v1/mihomo/dashboard',signal,'MIHOMO_NETWORK_UNAVAILABLE','MIHOMO_DASHBOARD_RESPONSE_INVALID','MIHOMO_DASHBOARD');if(!isMihomoDashboardStatus(response))throw new Error('MIHOMO_DASHBOARD_RESPONSE_INVALID');return response}
function isNotificationChannel(value:unknown):value is NotificationChannel{if(typeof value!=='object'||value===null)return false;const item=value as Partial<NotificationChannel>;return typeof item.id==='string'&&/^channel_[A-Za-z0-9_-]{22}$/.test(item.id)&&(item.provider==='wecom'||item.provider==='feishu')&&typeof item.displayName==='string'&&typeof item.webhookHint==='string'&&typeof item.signingSecretConfigured==='boolean'&&typeof item.enabled==='boolean'&&Array.isArray(item.eventKinds)&&typeof item.lastDeliveryAt==='string'&&['never','success','failed'].includes(item.lastDeliveryStatus??'')&&typeof item.lastErrorCode==='string'}
export async function listNotificationChannels(signal?:AbortSignal):Promise<NotificationChannel[]>{const response=await requestJSON('/api/v1/notification-channels',signal,'NOTIFICATION_NETWORK_UNAVAILABLE','NOTIFICATION_RESPONSE_INVALID','NOTIFICATION_LIST');if(typeof response!=='object'||response===null)throw new Error('NOTIFICATION_RESPONSE_INVALID');const channels=(response as {channels?:unknown}).channels;if(!Array.isArray(channels)||!channels.every(isNotificationChannel))throw new Error('NOTIFICATION_RESPONSE_INVALID');return channels}
export async function createNotificationChannel(request:NotificationChannelMutation,signal?:AbortSignal):Promise<NotificationChannel>{const response=await requestJSON('/api/v1/notification-channels',signal,'NOTIFICATION_NETWORK_UNAVAILABLE','NOTIFICATION_RESPONSE_INVALID','NOTIFICATION_CREATE',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});if(!isNotificationChannel(response))throw new Error('NOTIFICATION_RESPONSE_INVALID');return response}
export async function updateNotificationChannel(id:string,request:NotificationChannelMutation,signal?:AbortSignal):Promise<NotificationChannel>{const response=await requestJSON(`/api/v1/notification-channels/${encodeURIComponent(id)}`,signal,'NOTIFICATION_NETWORK_UNAVAILABLE','NOTIFICATION_RESPONSE_INVALID','NOTIFICATION_UPDATE',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});if(!isNotificationChannel(response))throw new Error('NOTIFICATION_RESPONSE_INVALID');return response}
export async function deleteNotificationChannel(id:string,signal?:AbortSignal):Promise<void>{await requestJSON(`/api/v1/notification-channels/${encodeURIComponent(id)}`,signal,'NOTIFICATION_NETWORK_UNAVAILABLE','NOTIFICATION_RESPONSE_INVALID','NOTIFICATION_DELETE',{method:'DELETE'})}
export async function testNotificationChannel(id:string,signal?:AbortSignal):Promise<NotificationChannel>{const response=await requestJSON(`/api/v1/notification-channels/${encodeURIComponent(id)}/test`,signal,'NOTIFICATION_NETWORK_UNAVAILABLE','NOTIFICATION_RESPONSE_INVALID','NOTIFICATION_TEST',{method:'POST'});if(!isNotificationChannel(response))throw new Error('NOTIFICATION_RESPONSE_INVALID');return response}

export async function getSystemHealth(signal?: AbortSignal): Promise<HealthResponse> {
  const health = await requestJSON(
    '/api/v1/system/health',
    signal,
    'HEALTH_NETWORK_UNAVAILABLE',
    'HEALTH_RESPONSE_INVALID',
    'HEALTH',
  )
  if (!isHealthResponse(health)) throw new Error('HEALTH_RESPONSE_INVALID')
  return health
}

export async function getSetupInventory(signal?: AbortSignal): Promise<InventoryResponse> {
  const inventory = await requestJSON(
    '/api/v1/setup/inventory',
    signal,
    'SETUP_NETWORK_UNAVAILABLE',
    'INVENTORY_RESPONSE_INVALID',
    'SETUP_INVENTORY',
  )
  if (!isInventoryResponse(inventory)) throw new Error('INVENTORY_RESPONSE_INVALID')
  return inventory
}

export async function getSetupHardwareTopology(signal?: AbortSignal): Promise<HardwareTopologyResponse> {
  const topology = await requestJSON(
    '/api/v1/setup/hardware/topology',
    signal,
    'SETUP_NETWORK_UNAVAILABLE',
    'HARDWARE_TOPOLOGY_RESPONSE_INVALID',
    'SETUP_HARDWARE_TOPOLOGY',
  )
  if (!isHardwareTopologyResponse(topology)) throw new Error('HARDWARE_TOPOLOGY_RESPONSE_INVALID')
  return topology
}

export async function putSetupSubscriptionProfileAccessMode(
  profileId: string,
  accessMode: AccessMode,
  signal?: AbortSignal,
): Promise<InventoryResponse> {
  if (!idPattern.test(profileId) || !isAccessMode(accessMode)) throw new Error('ACCESS_MODE_REQUEST_INVALID')
  const inventory = await requestJSON(
    `/api/v1/setup/subscription-profiles/${encodeURIComponent(profileId)}/access-mode`,
    signal,
    'ACCESS_MODE_NETWORK_UNAVAILABLE',
    'INVENTORY_RESPONSE_INVALID',
    'SETUP_ACCESS_MODE',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ accessMode }),
    },
  )
  if (!isInventoryResponse(inventory)) throw new Error('INVENTORY_RESPONSE_INVALID')
  return inventory
}

export async function confirmSetupHardware(signal?: AbortSignal): Promise<SetupSessionResponse> {
  const session = await requestJSON(
    '/api/v1/setup/hardware/confirm',
    signal,
    'SETUP_NETWORK_UNAVAILABLE',
    'SETUP_SESSION_RESPONSE_INVALID',
    'SETUP_HARDWARE',
    { method: 'POST' },
  )
  if (!isSetupSessionResponse(session)) throw new Error('SETUP_SESSION_RESPONSE_INVALID')
  return session
}

export async function getInventory(signal?: AbortSignal): Promise<InventoryResponse> {
  const inventory = await requestJSON(
    '/api/v1/inventory',
    signal,
    'INVENTORY_NETWORK_UNAVAILABLE',
    'INVENTORY_RESPONSE_INVALID',
    'INVENTORY',
  )
  if (!isInventoryResponse(inventory)) throw new Error('INVENTORY_RESPONSE_INVALID')
  return inventory
}

export async function getHardwareTopology(signal?: AbortSignal): Promise<HardwareTopologyResponse> {
  const topology = await requestJSON(
    '/api/v1/hardware/topology',
    signal,
    'HARDWARE_TOPOLOGY_NETWORK_UNAVAILABLE',
    'HARDWARE_TOPOLOGY_RESPONSE_INVALID',
    'HARDWARE_TOPOLOGY',
  )
  if (!isHardwareTopologyResponse(topology)) throw new Error('HARDWARE_TOPOLOGY_RESPONSE_INVALID')
  return topology
}

export async function putSubscriptionProfileAccessMode(
  profileId: string,
  accessMode: AccessMode,
  signal?: AbortSignal,
): Promise<InventoryResponse> {
  if (!idPattern.test(profileId) || !isAccessMode(accessMode)) throw new Error('ACCESS_MODE_REQUEST_INVALID')
  const inventory = await requestJSON(
    `/api/v1/subscription-profiles/${encodeURIComponent(profileId)}/access-mode`,
    signal,
    'ACCESS_MODE_NETWORK_UNAVAILABLE',
    'INVENTORY_RESPONSE_INVALID',
    'ACCESS_MODE',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ accessMode }),
    },
  )
  if (!isInventoryResponse(inventory)) throw new Error('INVENTORY_RESPONSE_INVALID')
  return inventory
}

export type SMSHistory = { messages: SMSMessage[]; totalCount: number; capacity: number; nearCapacity: boolean }

export async function listSMSHistory(signal?: AbortSignal): Promise<SMSHistory> {
  const response = await requestJSON(
    '/api/v1/messages',
    signal,
    'MESSAGE_NETWORK_UNAVAILABLE',
    'MESSAGE_HISTORY_RESPONSE_INVALID',
    'MESSAGE_HISTORY',
  )
  if (typeof response !== 'object' || response === null) throw new Error('MESSAGE_HISTORY_RESPONSE_INVALID')
  const candidate = response as Partial<SMSMessageListResponse>
  if (!Array.isArray(candidate.messages) || candidate.messages.length > 50 || !candidate.messages.every(isSMSMessage) ||
    !Number.isSafeInteger(candidate.totalCount) || (candidate.totalCount ?? -1) < candidate.messages.length ||
    !Number.isSafeInteger(candidate.capacity) || (candidate.capacity ?? 0) < 1 ||
    typeof candidate.nearCapacity !== 'boolean' || candidate.nearCapacity !== ((candidate.totalCount ?? 0) >= (candidate.capacity ?? 1) * 0.8)) {
    throw new Error('MESSAGE_HISTORY_RESPONSE_INVALID')
  }
  const ids = new Set(candidate.messages.map((message) => message.id))
  const operationIds = new Set(candidate.messages.map((message) => message.operationId))
  if (ids.size !== candidate.messages.length || operationIds.size !== candidate.messages.length) {
    throw new Error('MESSAGE_HISTORY_RESPONSE_INVALID')
  }
  return candidate as SMSHistory
}

export async function listSMSMessages(signal?: AbortSignal): Promise<SMSMessage[]> {
  return (await listSMSHistory(signal)).messages
}

export async function sendSMSMessage(request: SendSMSRequest, signal?: AbortSignal): Promise<SMSMessage> {
  if (
    !operationIdPattern.test(request.operationId) ||
    !idPattern.test(request.lineId) ||
    !destinationPattern.test(request.destination) ||
    request.body.trim().length === 0 ||
    [...request.body].length > 1600
  ) throw new Error('MESSAGE_REQUEST_INVALID')
  const response = await requestJSON(
    '/api/v1/messages',
    signal,
    'MESSAGE_NETWORK_UNAVAILABLE',
    'MESSAGE_RESPONSE_INVALID',
    'MESSAGE_SEND',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
    },
  )
  if (!isSMSMessage(response)) throw new Error('MESSAGE_RESPONSE_INVALID')
  return response
}

export async function deleteSMSMessage(messageId: string, signal?: AbortSignal): Promise<void> {
  if (!operationIdPattern.test(messageId)) throw new Error('MESSAGE_REQUEST_INVALID')
  await requestJSON(`/api/v1/messages/${encodeURIComponent(messageId)}`, signal, 'MESSAGE_NETWORK_UNAVAILABLE', 'MESSAGE_RESPONSE_INVALID', 'MESSAGE_DELETE', { method: 'DELETE' })
}

export async function listContacts(signal?: AbortSignal): Promise<Contact[]> {
  const response = await requestJSON('/api/v1/contacts', signal, 'CONTACT_NETWORK_UNAVAILABLE', 'CONTACT_RESPONSE_INVALID', 'CONTACT_LIST')
  if (typeof response !== 'object' || response === null) throw new Error('CONTACT_RESPONSE_INVALID')
  const candidate = response as Partial<ContactListResponse>
  if (!Array.isArray(candidate.contacts) || candidate.contacts.length > 1000 || !candidate.contacts.every(isContact)) {
    throw new Error('CONTACT_RESPONSE_INVALID')
  }
  if (new Set(candidate.contacts.map((contact) => contact.id)).size !== candidate.contacts.length) throw new Error('CONTACT_RESPONSE_INVALID')
  return candidate.contacts
}

function validateContactMutation(request: ContactMutationRequest) {
  if (request.displayName.trim().length === 0 || [...request.displayName.trim()].length > 80 || !destinationPattern.test(request.phoneNumber.trim())) {
    throw new Error('CONTACT_REQUEST_INVALID')
  }
}

export async function createContact(request: ContactMutationRequest, signal?: AbortSignal): Promise<Contact> {
  validateContactMutation(request)
  const response = await requestJSON('/api/v1/contacts', signal, 'CONTACT_NETWORK_UNAVAILABLE', 'CONTACT_RESPONSE_INVALID', 'CONTACT_CREATE', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request),
  })
  if (!isContact(response)) throw new Error('CONTACT_RESPONSE_INVALID')
  return response
}

export async function updateContact(id: string, request: ContactMutationRequest, signal?: AbortSignal): Promise<Contact> {
  if (!contactIdPattern.test(id)) throw new Error('CONTACT_REQUEST_INVALID')
  validateContactMutation(request)
  const response = await requestJSON(`/api/v1/contacts/${encodeURIComponent(id)}`, signal, 'CONTACT_NETWORK_UNAVAILABLE', 'CONTACT_RESPONSE_INVALID', 'CONTACT_UPDATE', {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request),
  })
  if (!isContact(response)) throw new Error('CONTACT_RESPONSE_INVALID')
  return response
}

export async function deleteContact(id: string, signal?: AbortSignal): Promise<void> {
  if (!contactIdPattern.test(id)) throw new Error('CONTACT_REQUEST_INVALID')
  await requestJSON(`/api/v1/contacts/${encodeURIComponent(id)}`, signal, 'CONTACT_NETWORK_UNAVAILABLE', 'CONTACT_RESPONSE_INVALID', 'CONTACT_DELETE', { method: 'DELETE' })
}

export async function listCalls(signal?: AbortSignal): Promise<Call[]> {
  const response = await requestJSON('/api/v1/calls', signal, 'CALL_NETWORK_UNAVAILABLE', 'CALL_RESPONSE_INVALID', 'CALL_LIST')
  if (typeof response !== 'object' || response === null) throw new Error('CALL_RESPONSE_INVALID')
  const candidate = response as Partial<CallListResponse>
  if (!Array.isArray(candidate.calls) || candidate.calls.length > 100 || !candidate.calls.every(isCall)) throw new Error('CALL_RESPONSE_INVALID')
  return candidate.calls
}

async function startCall(path: string, request: CallStartRequest, signal?: AbortSignal): Promise<Call> {
  if (!operationIdPattern.test(request.operationId) || !idPattern.test(request.lineId) || !destinationPattern.test(request.remoteAddress)) throw new Error('CALL_REQUEST_INVALID')
  const response = await requestJSON(path, signal, 'CALL_NETWORK_UNAVAILABLE', 'CALL_RESPONSE_INVALID', 'CALL_START', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request),
  })
  if (!isCall(response)) throw new Error('CALL_RESPONSE_INVALID')
  return response
}
export const dialCall = (request: CallStartRequest, signal?: AbortSignal) => startCall('/api/v1/calls/dial', request, signal)
export const simulateIncomingCall = (request: CallStartRequest, signal?: AbortSignal) => startCall('/api/v1/calls/incoming', request, signal)

export async function controlCall(id: string, request: CallActionRequest, signal?: AbortSignal): Promise<Call> {
  if (!callIdPattern.test(id) || !['answer', 'reject', 'hangup', 'dtmf'].includes(request.action) || (request.action === 'dtmf' && !/^[0-9*#A-D]{1,32}$/.test(request.digits ?? ''))) throw new Error('CALL_REQUEST_INVALID')
  const response = await requestJSON(`/api/v1/calls/${encodeURIComponent(id)}/action`, signal, 'CALL_NETWORK_UNAVAILABLE', 'CALL_RESPONSE_INVALID', 'CALL_ACTION', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request),
  })
  if (!isCall(response)) throw new Error('CALL_RESPONSE_INVALID')
  return response
}

function validateEUICCState(value: unknown): EUICCState {
  if (typeof value !== 'object' || value === null) throw new Error('EUICC_RESPONSE_INVALID')
  const candidate = value as Partial<EUICCState>
  if (typeof candidate.eidHint !== 'string' || candidate.eidHint.length === 0 || !Array.isArray(candidate.profiles) || candidate.profiles.length < 1 || candidate.profiles.length > 16 || candidate.profiles.filter((profile) => profile.active).length !== 1 || candidate.profiles.some((profile) => !/^simulator-euicc-profile-[ab]$/.test(profile.id) || !profile.displayName || !profile.displayIdentityHint || typeof profile.active !== 'boolean')) throw new Error('EUICC_RESPONSE_INVALID')
  return candidate as EUICCState
}
export async function getEUICCState(signal?: AbortSignal): Promise<EUICCState> { return validateEUICCState(await requestJSON('/api/v1/euicc', signal, 'EUICC_NETWORK_UNAVAILABLE', 'EUICC_RESPONSE_INVALID', 'EUICC')) }
export async function activateEUICCProfile(id: string, signal?: AbortSignal): Promise<EUICCState> {
  if (!/^simulator-euicc-profile-[ab]$/.test(id)) throw new Error('EUICC_REQUEST_INVALID')
  return validateEUICCState(await requestJSON(`/api/v1/euicc/profiles/${encodeURIComponent(id)}/activate`, signal, 'EUICC_NETWORK_UNAVAILABLE', 'EUICC_RESPONSE_INVALID', 'EUICC_SWITCH', { method:'POST' }))
}
function isAccessPath(value: unknown): value is AccessPathState { if(typeof value!=='object'||value===null)return false;const item=value as Partial<AccessPathState>;return /^simulator-line-[12]$/.test(item.lineId??'')&&['direct','mihomo-required'].includes(item.mode??'')&&['running','stopped','failed'].includes(item.mihomoState??'')&&['online','offline'].includes(item.lineState??'')&&item.authentication==='simulated-aka-complete'&&['connected','blocked'].includes(item.epdg??'')&&['registered','offline'].includes(item.ims??'')&&item.directFallback===false&&!(item.mode==='mihomo-required'&&item.mihomoState!=='running'&&item.lineState!=='offline') }
export async function listAccessPaths(signal?:AbortSignal):Promise<AccessPathState[]>{const response=await requestJSON('/api/v1/access-paths',signal,'ACCESS_PATH_NETWORK_UNAVAILABLE','ACCESS_PATH_RESPONSE_INVALID','ACCESS_PATH');if(typeof response!=='object'||response===null)throw new Error('ACCESS_PATH_RESPONSE_INVALID');const candidate=response as Partial<AccessPathListResponse>;if(!Array.isArray(candidate.lines)||candidate.lines.length>2||!candidate.lines.every(isAccessPath))throw new Error('ACCESS_PATH_RESPONSE_INVALID');return candidate.lines}
export async function configureAccessPath(lineId:string,request:AccessPathRequest,signal?:AbortSignal):Promise<AccessPathState>{if(!/^simulator-line-[12]$/.test(lineId)||!['direct','mihomo-required'].includes(request.mode)||!['running','stopped','failed'].includes(request.mihomoState))throw new Error('ACCESS_PATH_REQUEST_INVALID');const response=await requestJSON(`/api/v1/access-paths/${encodeURIComponent(lineId)}`,signal,'ACCESS_PATH_NETWORK_UNAVAILABLE','ACCESS_PATH_RESPONSE_INVALID','ACCESS_PATH_UPDATE',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});if(!isAccessPath(response))throw new Error('ACCESS_PATH_RESPONSE_INVALID');return response}
