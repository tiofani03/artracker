import { defineConfig } from '@playwright/test'

// End-to-end checks of the screenshot replay renderer and player. They run against the Vite dev server on a
// dedicated port and use the system Chrome (no browser download). `npm run test:e2e`.
export default defineConfig({
  testDir: 'e2e',
  testMatch: /.*\.e2e\.ts/,
  timeout: 120_000,
  fullyParallel: false,
  workers: 1,
  reporter: 'list',
  use: {
    channel: 'chrome',
    baseURL: 'http://localhost:5199',
    launchOptions: { args: ['--enable-precise-memory-info', '--js-flags=--expose-gc'] },
  },
  webServer: {
    command: 'npx vite --port 5199 --strictPort',
    url: 'http://localhost:5199/harness/screen.html',
    reuseExistingServer: true,
    timeout: 60_000,
  },
})
