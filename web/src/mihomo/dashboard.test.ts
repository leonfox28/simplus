import { describe, expect, it } from 'vitest'
import { zashboardLaunchURL } from './dashboard'

describe('zashboardLaunchURL', () => {
  it('creates the official direct setup URL with controller credentials in the fragment', () => {
    const result = zashboardLaunchURL({
      available: true,
      version: 'v3.6.0',
      controllerAddress: '192.168.50.10:19090',
      url: 'http://192.168.50.10:19090/ui/',
      secret: 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ',
    })

    expect(result).toBe(
      'http://192.168.50.10:19090/ui/#/setup?hostname=192.168.50.10&port=19090&secret=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ&disableUpgradeCore=1',
    )
  })

  it('does not create a link for an unavailable dashboard', () => {
    expect(zashboardLaunchURL(undefined)).toBeUndefined()
  })

  it('uses the browser host for a wildcard controller', () => {
    const result = zashboardLaunchURL(
      {
        available: true,
        version: 'v3.6.0',
        controllerAddress: '0.0.0.0:19090',
        url: 'http://0.0.0.0:19090/ui/',
        secret: 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ',
      },
      '192.168.50.25',
    )

    expect(result).toBe(
      'http://192.168.50.25:19090/ui/#/setup?hostname=192.168.50.25&port=19090&secret=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ&disableUpgradeCore=1',
    )
  })
})
