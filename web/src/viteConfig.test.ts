import path from 'node:path'
import type { ProxyOptions } from 'vite'
import { loadConfigFromFile } from 'vite'
import { describe, expect, it } from 'vitest'

describe('Vite development proxy', () => {
  it('preserves the trusted LAN Host used by setup completion URLs', async () => {
    const loaded = await loadConfigFromFile(
      { command: 'serve', mode: 'test' },
      path.resolve(process.cwd(), 'vite.config.ts'),
    )
    const proxy = loaded?.config.server?.proxy as Record<string, string | ProxyOptions> | undefined
    expect(proxy?.['/api']).toMatchObject({
      target: expect.any(String),
      changeOrigin: false,
    })
  })
})
