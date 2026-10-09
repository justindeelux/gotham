import { defineConfig } from "@playwright/test";

// Standalone config for backend-free CSS checks: static file fixture only,
// no webServer, no global setup, no baseURL. Run with:
//   npm run test:css
export default defineConfig({
  testDir: ".",
  testMatch: [
    "add-resource-picker.spec.ts",
    "environment-layout.spec.ts",
    "form-feedback.spec.ts",
    "form-layout.spec.ts",
    "i18n-account-layout.spec.ts",
    "i18n-settings-layout.spec.ts",
    "i18n-settings-live.spec.ts",
    "modal-scroll.spec.ts",
    "profile-layout.spec.ts",
    "projects-layout.spec.ts",
    "services-templates-layout.spec.ts",
    "sessions-layout.spec.ts",
    "shared-variables-layout.spec.ts",
  ],
  workers: 1,
  timeout: 30_000,
  reporter: [["line"]],
  use: {
    headless: true,
  },
});
