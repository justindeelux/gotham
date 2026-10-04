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
  ...featureBoundaries(),
];

// Feature-module boundaries (F1 web layout, JUS-24): shared/ never imports
// features/ or app/; a feature reaches into another feature only through its
// index (`@/features/<other>`) or its components
// (`@/features/<other>/components/*`) — never api, stores, pages, utils or
// composables directly. The app shell (src/app, including the router's lazy
// page imports) is outside these blocks, so no router override is needed.
// Only type-only edges remain between features (erased at build); there are
// no runtime import cycles.
function featureBoundaries() {
  const featureNames = [
    "auth",
    "dashboard",
    "servers",
    "applications",
    "databases",
    "services",
    "templates",
    "domains",
    "teams",
    "notifications",
    "version",
    "profile",
  ];
  const privateDirs = ["api", "stores", "pages", "utils", "composables"];
  const blocks = [
    {
      files: ["src/shared/**/*"],
      rules: {
        "no-restricted-imports": [
          "error",
          {
            patterns: [
              {
                group: ["@/features/**", "@/app/**"],
                message: "shared/ must not import features/ or app/; move the shared piece or the importer.",
              },
            ],
          },
        ],
      },
    },
  ];
  for (const name of featureNames) {
    blocks.push({
      files: [`src/features/${name}/**/*`],
      rules: {
        "no-restricted-imports": [
          "error",
          {
            patterns: featureNames
              .filter((other) => other !== name)
              .flatMap((other) =>
                privateDirs.flatMap((dir) => [
                  {
                    group: [`@/features/${other}/${dir}`, `@/features/${other}/${dir}/**`],
                    message: `import feature ${other} through its index (@/features/${other}) or its components/ instead.`,
                  },
                ]),
              ),
          },
        ],
      },
    });
  }
  return blocks;
}
