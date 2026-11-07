import { defineConfig, devices } from "@playwright/test"

// The server is started by scripts/e2e.sh (a temporary config and the seed account), so the
// tests only need its address. Flaky tests are bugs to fix, so there are no retries.
const baseURL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:18080"

export default defineConfig({
  testDir: "./e2e",
  // The large network checks need a seeded network of hundreds of devices: scripts/e2e.sh
  // sets E2E_LARGE for them, and the other specs expect the small demo network.
  ...(process.env.E2E_LARGE ? { testMatch: /large\.spec\.ts/ } : { testIgnore: /large\.spec\.ts/ }),
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  // Password hashing is slow on a shared runner; a longer wait is not a sleep, it ends as soon as the page is ready.
  expect: { timeout: 15_000 },
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
  outputDir: "./e2e-results",
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
})
