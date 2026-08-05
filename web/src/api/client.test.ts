import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  completeSetup,
  activateVoWiFiLine,
  confirmSetupHardware,
  confirmSetupHTTPS,
  consumeSetupBootstrap,
  createMihomoSubscription,
  getAuthSession,
  getHardwareTopology,
  getInventory,
  getSetupHTTPSRootCertificate,
  getSetupHardwareTopology,
  getSetupInventory,
  getSetupSession,
  getSetupStatus,
  getSystemHealth,
  listLineEgressBindings,
  listVoWiFiLines,
  listSMSMessages,
  login,
  logout,
  putLineEgressBinding,
  putSetupAdministrator,
  putSetupHTTPS,
  putSetupStorage,
  putSetupSubscriptionProfileAccessMode,
  putSubscriptionProfileAccessMode,
  deactivateVoWiFiLine,
  sendSMSMessage,
} from './client'

afterEach(() => vi.unstubAllGlobals())

describe('setup API client', () => {
  const setupStatus = {
    installationState: 'uninitialized',
    phase: 'bootstrap-required',
    setupRequired: true,
    businessApiAvailable: false,
    bootstrapGenerationAvailable: false,
    supportedFlows: ['create-new'],
  }

  it('accepts the fail-closed first-run boundary', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json(setupStatus)))

    await expect(getSetupStatus()).resolves.toEqual(setupStatus)
  })

  it('rejects contradictory setup state', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(Response.json({ ...setupStatus, businessApiAvailable: true })),
    )

    await expect(getSetupStatus()).rejects.toThrow('SETUP_RESPONSE_INVALID')
  })

  it('normalizes setup transport failures', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))

    await expect(getSetupStatus()).rejects.toThrow('SETUP_NETWORK_UNAVAILABLE')
  })

  const bootstrapCode = 'ERERERERERERERERERERERERERERERERERERERERERE'
  const setupSession = {
    authorized: true,
    expiresAt: '2026-08-02T12:30:00Z',
    selectedFlow: 'create-new',
    supportedFlows: ['create-new'],
    administratorConfigured: false,
    administratorUsername: '',
    instanceDefaultLocale: 'en-US',
    storageConfigured: false,
    dataRoot: '/srv/simplus/data',
    recordingsRoot: '/srv/simplus/data/recordings',
    httpsConfigured: false,
    httpsConfirmed: false,
    httpsMode: '',
    httpsListenUrl: '',
    httpsRootFingerprint: '',
    httpsLeafNotAfter: '',
    hardwareReviewed: false,
    hardwareDeviceCount: 0,
    hardwareLineCount: 0,
    hardwareInventoryDigest: '',
  }

  it('exchanges a validated fragment credential without returning it', async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json(setupSession))
    vi.stubGlobal('fetch', fetchMock)

    await expect(consumeSetupBootstrap(bootstrapCode)).resolves.toEqual(setupSession)
    const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(path).toBe('/api/v1/setup/bootstrap')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ bootstrapCode }))
  })

  it('rejects a malformed bootstrap credential before making a request', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    await expect(consumeSetupBootstrap('short')).rejects.toThrow('BOOTSTRAP_REQUEST_INVALID')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('persists administrator credentials without accepting an invalid response shape', async () => {
    const configuredSession = {
      ...setupSession,
      administratorConfigured: true,
      administratorUsername: 'leon',
      instanceDefaultLocale: 'zh-CN',
    }
    const fetchMock = vi.fn().mockResolvedValue(Response.json(configuredSession))
    vi.stubGlobal('fetch', fetchMock)
    const request = {
      username: 'Leon',
      password: 'correct horse battery staple',
      passwordConfirmation: 'correct horse battery staple',
      instanceDefaultLocale: 'zh-CN' as const,
    }

    await expect(putSetupAdministrator(request)).resolves.toEqual(configuredSession)
    const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(path).toBe('/api/v1/setup/administrator')
    expect(init.method).toBe('PUT')
    expect(init.body).toBe(JSON.stringify(request))
  })

  it('persists validated setup storage roots', async () => {
    const configuredSession = {
      ...setupSession,
      administratorConfigured: true,
      administratorUsername: 'leon',
      storageConfigured: true,
    }
    const fetchMock = vi.fn().mockResolvedValue(Response.json(configuredSession))
    vi.stubGlobal('fetch', fetchMock)
    const request = { recordingsRoot: '/srv/simplus/data/recordings' }

    await expect(putSetupStorage(request)).resolves.toEqual(configuredSession)
    const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(path).toBe('/api/v1/setup/storage')
    expect(init.method).toBe('PUT')
    expect(init.body).toBe(JSON.stringify(request))
  })

  it('configures and confirms a local-CA HTTPS candidate', async () => {
    const fingerprint = Array.from({ length: 32 }, () => 'AA').join(':')
    const candidate = {
      ...setupSession,
      administratorConfigured: true,
      administratorUsername: 'leon',
      storageConfigured: true,
      httpsConfigured: true,
      httpsMode: 'local-ca',
      httpsListenUrl: 'https://192.168.50.10:8443',
      httpsRootFingerprint: fingerprint,
      httpsLeafNotAfter: '2026-10-31T12:00:00Z',
    }
    const confirmed = { ...candidate, httpsConfirmed: true }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(Response.json(candidate))
      .mockResolvedValueOnce(Response.json({ pem: '-----BEGIN CERTIFICATE-----\nroot\n-----END CERTIFICATE-----\n', rootFingerprintSha256: fingerprint }))
      .mockResolvedValueOnce(Response.json(confirmed))
    vi.stubGlobal('fetch', fetchMock)
    const request = { mode: 'local-ca' as const, listenHost: '192.168.50.10', listenPort: 8443, subjectAlternativeNames: ['simplus.local'] }

    await expect(putSetupHTTPS(request)).resolves.toEqual(candidate)
    await expect(getSetupHTTPSRootCertificate()).resolves.toMatchObject({ rootFingerprintSha256: fingerprint })
    await expect(confirmSetupHTTPS(fingerprint)).resolves.toEqual(confirmed)
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/setup/https')
    expect(fetchMock.mock.calls[1]?.[0]).toBe('/api/v1/setup/https/root-certificate')
    expect(fetchMock.mock.calls[2]?.[0]).toBe('/api/v1/setup/https/confirm')
  })

  it('uses restricted inventory and access-mode endpoints during setup', async () => {
    const inventory = {
      generation: 1,
      revision: 'a'.repeat(64),
      observedAt: '2026-08-02T12:00:00Z',
      devices: [{ id: 'simulator-device-1', displayName: 'Simulator', transport: 'simulated', state: 'available', generation: 1, modemFunctionCount: 1, simSlotCount: 1, resourceGroupCount: 1 }],
      lines: [{ id: 'simulator-line-1', physicalDeviceId: 'simulator-device-1', subscriptionProfileId: 'simulator-profile-1', displayName: 'Simulator line', generation: 1, accessMode: 'hold-rf-off', accessModeConfigured: false, state: 'awaiting-access-mode', rfSafety: 'off' }],
    }
    const capabilities = {
      simAccess: true,
      sms: true,
      cellularVoice: true,
      digitalVoiceMedia: true,
      usbUac: false,
      simApdu: true,
      hostVoWifiAuth: true,
      rfControl: true,
      networkScan: true,
      manualNetworkSelection: true,
      primarySimLockState: true,
      pin1Verify: true,
      puk1Unblock: true,
      euiccProfiles: true,
    }
    const topology = {
      generation: 1,
      revision: 'a'.repeat(64),
      observedAt: '2026-08-02T12:00:00Z',
      devices: [{ id: 'simulator-device-1', displayName: 'Simulator', transport: 'simulated', state: 'available', generation: 1 }],
      modemFunctions: [{ id: 'simulator-function-1', physicalDeviceId: 'simulator-device-1', displayName: 'Simulator function', backend: 'simulated', generation: 1, capabilities }],
      simSlots: [{ id: 'simulator-slot-1', physicalDeviceId: 'simulator-device-1', index: 0, presence: 'present', activeMediaId: 'simulator-media-1', generation: 1 }],
      simMedia: [{ id: 'simulator-media-1', simSlotId: 'simulator-slot-1', kind: 'removable-euicc', identityState: 'known', displayIdentityHint: 'eUICC •••• 0001', generation: 1 }],
      subscriptionProfiles: [{ id: 'simulator-profile-1', simMediaId: 'simulator-media-1', displayName: 'Simulator profile 1', state: 'active', displayIdentityHint: 'ICCID •••• 0001', generation: 1, accessMode: 'hold-rf-off', accessModeConfigured: false }],
      resourceGroups: [{ id: 'simulator-resource-group-1', physicalDeviceId: 'simulator-device-1', displayName: 'Simulator shared modem resources', resources: ['radio-control', 'sim-access', 'voice-media', 'sms-storage', 'sim-apdu', 'host-vowifi-auth', 'network-selection', 'sim-lock', 'euicc-profiles'], modemFunctionIds: ['simulator-function-1'], simSlotIds: ['simulator-slot-1'], maxActiveCalls: 1, maxConcurrentOps: 1, generation: 1 }],
      lines: [{ id: 'simulator-line-1', physicalDeviceId: 'simulator-device-1', modemFunctionId: 'simulator-function-1', subscriptionProfileId: 'simulator-profile-1', resourceGroupId: 'simulator-resource-group-1', displayName: 'Simulator line', generation: 1, capabilities, accessMode: 'hold-rf-off', accessModeConfigured: false, state: 'awaiting-access-mode', rfSafety: 'off' }],
    }
    const updated = { ...inventory, revision: 'b'.repeat(64), lines: [{ ...inventory.lines[0], accessModeConfigured: true, state: 'ready' }] }
    const reviewed = {
      ...setupSession,
      httpsConfigured: true,
      httpsConfirmed: true,
      httpsMode: 'loopback-only',
      httpsListenUrl: 'http://127.0.0.1:8080',
      hardwareReviewed: true,
      hardwareDeviceCount: 1,
      hardwareLineCount: 1,
      hardwareInventoryDigest: 'a'.repeat(64),
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(Response.json(inventory))
      .mockResolvedValueOnce(Response.json(topology))
      .mockResolvedValueOnce(Response.json(updated))
      .mockResolvedValueOnce(Response.json(reviewed))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getSetupInventory()).resolves.toEqual(inventory)
    await expect(getSetupHardwareTopology()).resolves.toEqual(topology)
    await expect(putSetupSubscriptionProfileAccessMode('simulator-profile-1', 'hold-rf-off')).resolves.toEqual(updated)
    await expect(confirmSetupHardware()).resolves.toEqual(reviewed)
    expect(fetchMock.mock.calls.map((call) => call[0])).toEqual([
      '/api/v1/setup/inventory',
      '/api/v1/setup/hardware/topology',
      '/api/v1/setup/subscription-profiles/simulator-profile-1/access-mode',
      '/api/v1/setup/hardware/confirm',
    ])
  })

  it('validates the atomic setup completion response', async () => {
    const completion = { installationState: 'ready', managementUrl: 'http://127.0.0.1:8080', loginRequired: true }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json(completion)))
    await expect(completeSetup()).resolves.toEqual(completion)
  })

  it('uses administrator auth endpoints and sends the double-submit CSRF header', async () => {
    const session = { username: 'admin', locale: 'en-US', expiresAt: '2026-08-03T00:00:00Z' }
    document.cookie = 'simplus_csrf=test-csrf; path=/'
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(Response.json(session))
      .mockResolvedValueOnce(Response.json(session))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(login({ username: 'admin', password: 'secret' })).resolves.toEqual(session)
    await expect(getAuthSession()).resolves.toEqual(session)
    await expect(logout()).resolves.toBeUndefined()
    expect(new Headers(fetchMock.mock.calls[2][1]?.headers).get('X-Simplus-CSRF')).toBe('test-csrf')
  })

  it('rejects a setup session that claims an administrator without an identity', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(Response.json({ ...setupSession, administratorConfigured: true })),
    )

    await expect(getSetupSession()).rejects.toThrow('SETUP_SESSION_RESPONSE_INVALID')
  })

  it('treats a missing restricted session as an unauthenticated setup page', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ code: 'SETUP_SESSION_UNAUTHORIZED', retryable: false }), {
          status: 401,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )

    await expect(getSetupSession()).resolves.toBeNull()
  })
})

describe('health API client', () => {
  it('accepts a valid generated-contract health response', async () => {
    const health = {
      status: 'ok',
      version: 'test',
      apiVersion: 'v1',
      installationState: 'uninitialized',
      rfSafety: 'off',
      backend: 'simulator',
      databaseCount: 5,
    }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json(health)))

    await expect(getSystemHealth()).resolves.toEqual(health)
  })

  it('rejects a successful response that violates the runtime contract', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({ status: 'ok' })))

    await expect(getSystemHealth()).rejects.toThrow('HEALTH_RESPONSE_INVALID')
  })

  it('normalizes malformed JSON from a successful response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{not-json', { status: 200 })))

    await expect(getSystemHealth()).rejects.toThrow('HEALTH_RESPONSE_INVALID')
  })

  it('uses the stable API error code when the server returns one', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ code: 'HEALTH_SNAPSHOT_UNAVAILABLE', retryable: true }), {
          status: 500,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )

    await expect(getSystemHealth()).rejects.toThrow('HEALTH_SNAPSHOT_UNAVAILABLE')
  })

  it('normalizes transport failures to a stable error code', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('browser-specific network text')))

    await expect(getSystemHealth()).rejects.toThrow('HEALTH_NETWORK_UNAVAILABLE')
  })

  it('falls back to a stable HTTP status code for an invalid error body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('not-json', { status: 503 })))

    await expect(getSystemHealth()).rejects.toThrow('HEALTH_HTTP_503')
  })
})

describe('inventory API client', () => {
  const inventory = {
    generation: 1,
    revision: 'a'.repeat(64),
    observedAt: '2026-08-02T12:00:00Z',
    devices: [
      {
        id: 'simulator-device-1',
        displayName: 'Simulator modem 1',
        transport: 'simulated',
        state: 'available',
        generation: 1,
        modemFunctionCount: 1,
        simSlotCount: 1,
        resourceGroupCount: 1,
      },
    ],
    lines: [
      {
        id: 'simulator-line-1',
        physicalDeviceId: 'simulator-device-1',
        subscriptionProfileId: 'simulator-profile-1',
        displayName: 'Simulator line 1',
        generation: 1,
        accessMode: 'hold-rf-off',
        accessModeConfigured: false,
        state: 'awaiting-access-mode',
        rfSafety: 'off',
      },
    ],
  }

  it('accepts a valid dynamic inventory response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json(inventory)))

    await expect(getInventory()).resolves.toEqual(inventory)
  })

  it('validates the full dynamic hardware topology and its cross-references', async () => {
    const capabilities = {
      simAccess: true,
      sms: true,
      cellularVoice: true,
      digitalVoiceMedia: true,
      usbUac: false,
      simApdu: true,
      hostVoWifiAuth: true,
      rfControl: true,
      networkScan: true,
      manualNetworkSelection: true,
      primarySimLockState: true,
      pin1Verify: true,
      puk1Unblock: true,
      euiccProfiles: false,
    }
    const topology = {
      generation: 1,
      revision: 'a'.repeat(64),
      observedAt: '2026-08-02T12:00:00Z',
      devices: [{ id: 'device-1', displayName: 'Device 1', transport: 'usb', state: 'available', generation: 1 }],
      modemFunctions: [{ id: 'function-1', physicalDeviceId: 'device-1', displayName: 'Function 1', backend: 'direct-qmi', generation: 1, capabilities }],
      simSlots: [{ id: 'slot-1', physicalDeviceId: 'device-1', index: 0, presence: 'present', activeMediaId: 'media-1', generation: 1 }],
      simMedia: [{ id: 'media-1', simSlotId: 'slot-1', kind: 'uicc', identityState: 'known', displayIdentityHint: 'SIM •••• 0101', generation: 1 }],
      subscriptionProfiles: [{ id: 'profile-1', simMediaId: 'media-1', displayName: 'Profile 1', state: 'active', displayIdentityHint: 'ICCID •••• 0101', generation: 1, accessMode: 'hold-rf-off', accessModeConfigured: true }],
      resourceGroups: [{ id: 'group-1', physicalDeviceId: 'device-1', displayName: 'Group 1', resources: ['radio-control', 'sim-access', 'voice-media', 'sms-storage', 'sim-apdu', 'host-vowifi-auth', 'network-selection', 'sim-lock'], modemFunctionIds: ['function-1'], simSlotIds: ['slot-1'], maxActiveCalls: 1, maxConcurrentOps: 1, generation: 1 }],
      lines: [{ id: 'line-1', physicalDeviceId: 'device-1', modemFunctionId: 'function-1', subscriptionProfileId: 'profile-1', resourceGroupId: 'group-1', displayName: 'Line 1', generation: 1, capabilities, accessMode: 'hold-rf-off', accessModeConfigured: true, state: 'ready', rfSafety: 'off' }],
    }
    const fetchMock = vi.fn().mockResolvedValue(Response.json(topology))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getHardwareTopology()).resolves.toEqual(topology)
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/hardware/topology')

    fetchMock.mockResolvedValueOnce(Response.json({
      ...topology,
      lines: [{ ...topology.lines[0], resourceGroupId: 'missing-group' }],
    }))
    await expect(getHardwareTopology()).rejects.toThrow('HARDWARE_TOPOLOGY_RESPONSE_INVALID')

    fetchMock.mockResolvedValueOnce(Response.json({
      ...topology,
      lines: [{ ...topology.lines[0], generation: 2 }],
    }))
    await expect(getHardwareTopology()).rejects.toThrow('HARDWARE_TOPOLOGY_RESPONSE_INVALID')

    fetchMock.mockResolvedValueOnce(Response.json({
      ...topology,
      generation: 2,
      subscriptionProfiles: [{ ...topology.subscriptionProfiles[0], generation: 2 }],
      lines: [{ ...topology.lines[0], generation: 2 }],
    }))
    await expect(getHardwareTopology()).rejects.toThrow('HARDWARE_TOPOLOGY_RESPONSE_INVALID')

    fetchMock.mockResolvedValueOnce(Response.json({
      ...topology,
      lines: [{ ...topology.lines[0], capabilities: { ...capabilities, euiccProfiles: true } }],
    }))
    await expect(getHardwareTopology()).rejects.toThrow('HARDWARE_TOPOLOGY_RESPONSE_INVALID')

    fetchMock.mockResolvedValueOnce(Response.json({
      ...topology,
      devices: [{ ...topology.devices[0], state: 'unavailable' }],
    }))
    await expect(getHardwareTopology()).rejects.toThrow('HARDWARE_TOPOLOGY_RESPONSE_INVALID')

    fetchMock.mockResolvedValueOnce(Response.json({
      ...topology,
      subscriptionProfiles: [topology.subscriptionProfiles[0], { ...topology.subscriptionProfiles[0], id: 'profile-2' }],
    }))
    await expect(getHardwareTopology()).rejects.toThrow('HARDWARE_TOPOLOGY_RESPONSE_INVALID')

    fetchMock.mockResolvedValueOnce(Response.json({ ...topology, unexpected: true }))
    await expect(getHardwareTopology()).rejects.toThrow('HARDWARE_TOPOLOGY_RESPONSE_INVALID')

    const noSIMAccess = { ...capabilities, simAccess: false, simApdu: false, hostVoWifiAuth: false, primarySimLockState: false, pin1Verify: false, puk1Unblock: false }
    fetchMock.mockResolvedValueOnce(Response.json({
      ...topology,
      modemFunctions: [{ ...topology.modemFunctions[0], capabilities: noSIMAccess }],
      lines: [{ ...topology.lines[0], capabilities: noSIMAccess }],
    }))
    await expect(getHardwareTopology()).rejects.toThrow('HARDWARE_TOPOLOGY_RESPONSE_INVALID')
  })

  it('rejects a line that references an unknown physical device', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        Response.json({
          ...inventory,
          lines: [{ ...inventory.lines[0], physicalDeviceId: 'missing-device' }],
        }),
      ),
    )

    await expect(getInventory()).rejects.toThrow('INVENTORY_RESPONSE_INVALID')
  })

  it('rejects an inconsistent unconfigured line', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        Response.json({
          ...inventory,
          lines: [{ ...inventory.lines[0], accessMode: 'cellular-native' }],
        }),
      ),
    )

    await expect(getInventory()).rejects.toThrow('INVENTORY_RESPONSE_INVALID')
  })

  it('normalizes inventory transport failures', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))

    await expect(getInventory()).rejects.toThrow('INVENTORY_NETWORK_UNAVAILABLE')
  })

  it('persists a selected access mode and validates the returned inventory', async () => {
    const updated = {
      ...inventory,
      revision: 'b'.repeat(64),
      lines: [
        {
          ...inventory.lines[0],
          accessMode: 'host-vowifi-only',
          accessModeConfigured: true,
          state: 'ready',
        },
      ],
    }
    const fetchMock = vi.fn().mockResolvedValue(Response.json(updated))
    vi.stubGlobal('fetch', fetchMock)

    await expect(putSubscriptionProfileAccessMode('simulator-profile-1', 'host-vowifi-only')).resolves.toEqual(updated)
    const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(path).toBe('/api/v1/subscription-profiles/simulator-profile-1/access-mode')
    expect(init.method).toBe('PUT')
    expect(init.body).toBe('{"accessMode":"host-vowifi-only"}')
    expect(new Headers(init.headers).get('Content-Type')).toBe('application/json')
  })

  it('normalizes access-mode transport failures', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))

    await expect(putSubscriptionProfileAccessMode('simulator-profile-1', 'hold-rf-off')).rejects.toThrow(
      'ACCESS_MODE_NETWORK_UNAVAILABLE',
    )
  })
})

describe('Mihomo subscription API client', () => {
  it('creates a subscription from its URL without inventing client-side identity fields', async () => {
    document.cookie = 'simplus_csrf=mihomo-csrf; path=/'
    const subscription = {
      id: 'subscription_abcdefghijklmnopqrstuv',
      displayName: 'ABC234',
      url: 'https://subscription.example/path',
      urlHint: 'subscription.example',
      enabled: true,
      selected: true,
      artifactReady: true,
      lastRefreshAt: '2026-08-04T12:00:00Z',
      lastRefreshStatus: 'success',
      nodeCount: 4,
      lastErrorCode: '',
    }
    const fetchMock = vi.fn().mockResolvedValue(Response.json(subscription, { status: 201 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(createMihomoSubscription({ url: subscription.url })).resolves.toEqual(subscription)
    const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(path).toBe('/api/v1/mihomo/subscriptions')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ url: subscription.url }))
    expect(new Headers(init.headers).get('X-Simplus-CSRF')).toBe('mihomo-csrf')
  })
})

describe('Line egress API client', () => {
  const binding = {
    lineId: 'simulator-line-1',
    mode: 'mihomo-country' as const,
    countryCode: 'GB',
    countryName: '英国',
    listenerPort: 20157,
    ready: true,
    readinessReason: 'READY' as const,
  }

  it('lists and updates a country binding without a subscription identifier', async () => {
    document.cookie = 'simplus_csrf=line-egress-csrf; path=/'
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(Response.json({ bindings: [binding] }))
      .mockResolvedValueOnce(Response.json(binding))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listLineEgressBindings()).resolves.toEqual([binding])
    await expect(putLineEgressBinding(binding.lineId, { mode: binding.mode, countryCode: 'GB' })).resolves.toEqual(binding)
    const [path, init] = fetchMock.mock.calls[1] as [string, RequestInit]
    expect(path).toBe('/api/v1/lines/simulator-line-1/egress')
    expect(init.method).toBe('PUT')
    expect(init.body).toBe('{"mode":"mihomo-country","countryCode":"GB"}')
    expect(new Headers(init.headers).get('X-Simplus-CSRF')).toBe('line-egress-csrf')
  })

  it('rejects an inconsistent direct binding before dispatch', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    await expect(putLineEgressBinding('simulator-line-1', { mode: 'direct', countryCode: 'GB' })).rejects.toThrow('LINE_EGRESS_REQUEST_INVALID')
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('Host VoWiFi API client', () => {
  const lineID = 'agent-line-0123456789abcdef0123456789abcdef'
  const online = {
    lineId: lineID,
    desiredActive: true,
    eligible: true,
    readinessCode: 'READY' as const,
    state: 'online' as const,
    stage: 'REGISTERED',
    online: true,
    egressMode: 'mihomo-country' as const,
    countryCode: 'GB',
    countryName: '英国',
    registeredAt: '2026-08-04T21:07:41Z',
    nextRefreshAt: '2026-08-04T21:32:41Z',
    attempt: 1,
    lastErrorCode: '',
  }

  it('lists state and sends CSRF-protected activation mutations', async () => {
    document.cookie = 'simplus_csrf=vowifi-csrf; path=/'
    const stopped = { ...online, desiredActive: false, state: 'stopped' as const, stage: '', online: false, registeredAt: '', nextRefreshAt: '' }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(Response.json({ lines: [online] }))
      .mockResolvedValueOnce(Response.json(online, { status: 202 }))
      .mockResolvedValueOnce(Response.json(stopped))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listVoWiFiLines()).resolves.toEqual([online])
    await expect(activateVoWiFiLine(lineID)).resolves.toEqual(online)
    await expect(deactivateVoWiFiLine(lineID)).resolves.toEqual(stopped)
    expect(fetchMock.mock.calls[1]?.[0]).toBe(`/api/v1/vowifi-lines/${lineID}/activate`)
    expect(fetchMock.mock.calls[2]?.[0]).toBe(`/api/v1/vowifi-lines/${lineID}/deactivate`)
    for (const call of fetchMock.mock.calls.slice(1)) {
      const init = call[1] as RequestInit
      expect(init.method).toBe('POST')
      expect(new Headers(init.headers).get('X-Simplus-CSRF')).toBe('vowifi-csrf')
    }
  })

  it('rejects contradictory online state', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({ lines: [{ ...online, online: false }] })))
    await expect(listVoWiFiLines()).rejects.toThrow('VOWIFI_RESPONSE_INVALID')
  })
})

describe('SMS API client', () => {
  const sentMessage = {
    id: 'msg_0123456789abcdef012345',
    operationId: 'operation-0123456789abcdef',
    direction: 'outbound',
    lineId: 'simulator-line-1',
    remoteAddress: '+8613800138000',
    body: 'hello simulator',
    status: 'sent',
    providerMessageId: 'sim_msg_0123456789abcdef012345',
    errorCode: '',
    createdAt: '2026-08-03T12:00:00Z',
    updatedAt: '2026-08-03T12:00:01Z',
    sentAt: '2026-08-03T12:00:01Z',
  }
  const receivedMessage = {
    id: 'msg_inbound012345678901234',
    operationId: 'inbound-operation-01234567',
    direction: 'inbound',
    lineId: 'simulator-line-1',
    remoteAddress: 'Simplus',
    body: 'welcome',
    status: 'received',
    providerMessageId: 'simulator-inbound-welcome-1',
    errorCode: '',
    createdAt: '2026-08-03T11:59:00Z',
    updatedAt: '2026-08-03T12:00:00Z',
  }

  it('validates history and sends a CSRF-protected typed request', async () => {
    document.cookie = 'simplus_csrf=sms-csrf; path=/'
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(Response.json({ messages: [sentMessage, receivedMessage], totalCount: 2, capacity: 10000, nearCapacity: false }))
      .mockResolvedValueOnce(Response.json(sentMessage, { status: 201 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listSMSMessages()).resolves.toEqual([sentMessage, receivedMessage])
    const request = {
      operationId: sentMessage.operationId,
      lineId: sentMessage.lineId,
      destination: sentMessage.remoteAddress,
      body: sentMessage.body,
    }
    await expect(sendSMSMessage(request)).resolves.toEqual(sentMessage)
    expect(fetchMock.mock.calls[1]?.[0]).toBe('/api/v1/messages')
    const init = fetchMock.mock.calls[1]?.[1] as RequestInit
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify(request))
    expect(new Headers(init.headers).get('X-Simplus-CSRF')).toBe('sms-csrf')
  })

  it('rejects contradictory terminal state and invalid requests', async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ messages: [{ ...sentMessage, sentAt: undefined }] }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listSMSMessages()).rejects.toThrow('MESSAGE_HISTORY_RESPONSE_INVALID')
    await expect(sendSMSMessage({
      operationId: sentMessage.operationId,
      lineId: sentMessage.lineId,
      destination: 'invalid',
      body: sentMessage.body,
    })).rejects.toThrow('MESSAGE_REQUEST_INVALID')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
