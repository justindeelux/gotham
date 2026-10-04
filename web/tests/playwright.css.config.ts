import { defineConfig } from "@playwright/test";

// Standalone config for backend-free CSS checks: static file fixture only,
// no webServer, no global setup, no baseURL. Run with:
//   npm run test:css
export default defineConfig({
  testDir: ".",
  testMatch: ["form-feedback.spec.ts", "form-layout.spec.ts"],
  workers: 1,
  timeout: 30_000,
  reporter: [["line"]],
  use: {
    headless: true,
  },
});
