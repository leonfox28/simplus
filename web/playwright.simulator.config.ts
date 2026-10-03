import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e-simulator',
  workers: 1,
  retries: 0,
  timeout: 60_000,
  use: { ...devices['Desktop Chrome'], baseURL: 'http://127.0.0.1:4183', trace: 'retain-on-failure' },
  webServer: {
    command: 'bash ../scripts/dev/browser-simulator.sh',
    url: 'http://127.0.0.1:4183/api/v1/system/health',
    reuseExistingServer: false,
    timeout: 180_000,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 40_000 },
  },
})
