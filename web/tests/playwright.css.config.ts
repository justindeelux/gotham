import { defineConfig } from "@playwright/test";

// Standalone config for backend-free CSS checks: static file fixture only,
// no webServer, no global setup, no baseURL. Run with:
//   npm run test:css
export default defineConfig({
  testDir: ".",
  testMatch: [
    "environment-layout.spec.ts",
    "form-feedback.spec.ts",
    "form-layout.spec.ts",
    "profile-layout.spec.ts",
    "projects-layout.spec.ts",
    "sessions-layout.spec.ts",
  ],
  workers: 1,
  timeout: 30_000,
  reporter: [["line"]],
  use: {
    headless: true,
  },
});
