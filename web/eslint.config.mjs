// ESLint flat config for the Gotham SPA (B3-19/B4-19).
//
// Minimal rule set: eslint:recommended for scripts plus the vue essential
// rules (correctness, not style). TypeScript files and Vue SFC script blocks
// are parsed with @typescript-eslint/parser without type-aware rules, so the
// gate stays fast and only flags real defects; vue-tsc remains the type
// authority. Run from web/: npm run lint
import js from "@eslint/js";
import tsParser from "@typescript-eslint/parser";
import pluginVue from "eslint-plugin-vue";
import globals from "globals";

export default [
  {
    ignores: [
      "dist/**",
      "test-results/**",
      "playwright-report/**",
      "e2e/.auth/**",
      // Ambient declarations (RouteMeta augmentation) are conventionally
      // unlinted; the no-unused-vars rule cannot see declaration merging.
      "**/*.d.ts",
    ],
  },
  js.configs.recommended,
  ...pluginVue.configs["flat/essential"],
  {
    // Leading-underscore params/vars are intentionally unused (callback
    // signatures, documentary interface names); the rule still catches every
    // other unused binding.
    rules: {
      "no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],
    },
  },
  {
    // Browser SPA source: timers, fetch, DOM and storage globals.
    files: ["src/**/*"],
    languageOptions: {
      globals: globals.browser,
    },
  },
  {
    // Specs, harness scripts and configs run in Node but also touch browser
    // APIs (page.evaluate bodies, URL/Response helpers).
    files: ["e2e/**/*", "scripts/**/*", "*.config.ts", "vite.config.ts"],
    languageOptions: {
      globals: { ...globals.browser, ...globals.node },
    },
  },
  {
    files: ["**/*.ts", "**/*.mts"],
    languageOptions: {
      parser: tsParser,
      sourceType: "module",
    },
  },
  {
    files: ["**/*.vue"],
    languageOptions: {
      // The outer SFC parser stays vue-eslint-parser (set by the essential
      // config); this only swaps the parser for <script lang="ts"> blocks.
      parserOptions: { parser: tsParser },
    },
  },
];
