import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: 'http://127.0.0.1:4173',
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium-desktop', grep: /@desktop/, use: { ...devices['Desktop Chrome'] } },
    { name: 'chromium-mobile', grep: /@mobile/, use: { ...devices['Pixel 7'] } },
  ],
  webServer: {
    command: 'PORT=4173 HOST=127.0.0.1 corepack pnpm dev',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
})
