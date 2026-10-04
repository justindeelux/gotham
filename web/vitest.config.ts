import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vitest/config";

// Minimal unit-test setup for form-feedback checks (JUS-16/17/18).
export default defineConfig({
  plugins: [vue()],
  test: {
    environment: "jsdom",
    include: ["tests/**/*.test.ts"],
  },
});
