import { fileURLToPath, URL } from "node:url";

import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vitest/config";

// Minimal unit-test setup for form-feedback checks (JUS-16/17/18).
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  test: {
    environment: "jsdom",
    include: ["tests/**/*.test.ts"],
    // Temporarily stages the feature-catalog discovery fixture under
    // src/features/ (removed in teardown); see the setup file.
    globalSetup: ["tests/i18n-discovery-global-setup.ts"],
  },
});
