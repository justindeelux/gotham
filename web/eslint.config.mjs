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
      "**/*.d.ts",
    ],
  },
  js.configs.recommended,
  ...pluginVue.configs["flat/essential"],
  {
    rules: {
      "no-empty": "off",
      "no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],
    },
  },
  {
    files: ["src/**/*"],
    languageOptions: {
      globals: globals.browser,
    },
  },
  {
    files: ["e2e/**/*", "scripts/**/*", "*.config.ts", "vite.config.ts"],
    languageOptions: {
      globals: { ...globals.browser, ...globals.node },
    },
  },
  {
    // globals only, so browser lib types never redeclare test-setup globals.
    files: ["tests/git-sources-*.test.ts"],
    languageOptions: {
      globals: globals.node,
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
      parserOptions: { parser: tsParser },
    },
  },
  ...featureBoundaries(),
];

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
    "users",
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
