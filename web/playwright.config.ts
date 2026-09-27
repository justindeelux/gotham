import { defineConfig, devices } from "@playwright/test";

/**
 * Playwright configuration for the Gotham browser smoke suite.
 *
 * The suite drives a real control plane: it never boots a web server itself.
 * `baseURL` points at a running `gotham serve` instance, which embeds the
 * built SPA. CI starts that process (see .github/workflows/ui-e2e.yml); for a
 * local run see docs/test-server.md.
 *
 * Chromium only, headless, single worker: the register/login endpoints are
 * rate-limited per IP (5 requests/minute with a burst of 5), and the whole run
 * shares one account created once in global setup.
 */
const baseURL = process.env.GOTHAM_E2E_BASE_URL ?? "http://127.0.0.1:8099";

export default defineConfig({
  testDir: "./e2e",
  globalSetup: "./e2e/global-setup.ts",
  outputDir: "./test-results",
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: [
    ["list"],
    ["html", { outputFolder: "playwright-report", open: "never" }],
  ],
  use: {
    baseURL,
    headless: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "off",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
