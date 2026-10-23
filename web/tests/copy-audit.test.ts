import { describe, expect, it } from "vitest";

import { leafEntries } from "@/shared/i18n/catalog";
import enCommon from "@/shared/i18n/locales/en";
import viCommon from "@/shared/i18n/locales/vi";
import editorEn from "@/features/files/locales/editorEn";
import editorVi from "@/features/files/locales/editorVi";

const DENYLIST: Array<{ label: string; pattern: RegExp }> = [
  { label: "repo file path", pattern: /\.md\b/ },
  { label: "repo directory", pattern: /deploy\// },
  { label: "curl command", pattern: /curl / },
  { label: "brew command", pattern: /brew / },
  { label: "apt install", pattern: /apt install/ },
  { label: "sudo command", pattern: /sudo / },
  { label: "feature env var", pattern: /FEATURE_/ },
  { label: "gotham env var", pattern: /GOTHAM_/ },
];

const ALLOWLIST: Record<string, string> = {
  "servers.wizard.installHowto [gotham env var]":
    "The install wizard's one-time manual agent bootstrap, kept as the explicit alternative; it names the installer's own env var on purpose.",
  "features/servers/components/AddServerWizard.vue [repo directory]":
    "The install wizard's one-time manual agent bootstrap command (the explicit alternative), which reaches the installer sibling files; it is shown verbatim in the copyable block.",
  "features/servers/components/AddServerWizard.vue [sudo command]":
    "The install wizard's one-time manual agent bootstrap command (the explicit alternative); `sudo` is part of the command the operator copies.",
  "features/servers/components/AddServerWizard.vue [gotham env var]":
    "The install wizard's one-time manual agent bootstrap command (the explicit alternative); it sets the installer's own env vars on purpose.",
};

interface Finding {
  id: string;
  label: string;
  where: string;
  value: string;
}

function findingKey(finding: Finding): string {
  return `${finding.id} [${finding.label}]`;
}

function matches(value: string, id: string, where: string): Finding[] {
  return DENYLIST.filter((rule) => rule.pattern.test(value)).map((rule) => ({
    id,
    label: rule.label,
    where,
    value,
  }));
}

function catalogFindings(): Finding[] {
  const out: Finding[] = [];
  const add = (
    module: string,
    catalog: Record<string, unknown>,
    where: string,
  ): void => {
    for (const [key, value] of leafEntries(catalog)) {
      if (typeof value === "string") {
        out.push(...matches(value, `${module}.${key}`, where));
      }
    }
  };

  const featureGlobs: Array<[Record<string, { default: Record<string, unknown> }>, "en" | "vi"]> = [
    [
      import.meta.glob<{ default: Record<string, unknown> }>(
        "@/features/*/locales/en.ts",
        { eager: true },
      ),
      "en",
    ],
    [
      import.meta.glob<{ default: Record<string, unknown> }>(
        "@/features/*/locales/vi.ts",
        { eager: true },
      ),
      "vi",
    ],
  ];
  for (const [modules, locale] of featureGlobs) {
    for (const [path, mod] of Object.entries(modules)) {
      const module = /features\/([^/]+)\/locales\/[a-z]+\.ts$/.exec(path)?.[1];
      if (module && mod?.default) {
        add(module, mod.default, locale);
      }
    }
  }

  add("common", enCommon as Record<string, unknown>, "en");
  add("common", viCommon as Record<string, unknown>, "vi");
  add("files.editor", editorEn as unknown as Record<string, unknown>, "en");
  add("files.editor", editorVi as unknown as Record<string, unknown>, "vi");

  return out;
}

const sourceFiles = {
  ...import.meta.glob<string>("@/features/**/*.{ts,vue}", {
    query: "?raw",
    import: "default",
    eager: true,
  }),
  ...import.meta.glob<string>("@/app/**/*.{ts,vue}", {
    query: "?raw",
    import: "default",
    eager: true,
  }),
  ...import.meta.glob<string>("@/shared/**/*.{ts,vue}", {
    query: "?raw",
    import: "default",
    eager: true,
  }),
} as Record<string, string>;

function contentLines(text: string): string[] {
  return text.split("\n").filter((line) => {
    const trimmed = line.trimStart();
    return !(
      trimmed.startsWith("//") ||
      trimmed.startsWith("*") ||
      trimmed.startsWith("/*") ||
      trimmed.startsWith("<!--")
    );
  });
}

function sourceFindings(): Finding[] {
  const out: Finding[] = [];
  for (const [path, raw] of Object.entries(sourceFiles)) {
    if (path.includes("/locales/")) {
      continue;
    }
    const id = path.replace(/^.*\/src\//, "");
    for (const rule of DENYLIST) {
      const hit = contentLines(raw).find((line) => rule.pattern.test(line));
      if (hit !== undefined) {
        out.push({ id, label: rule.label, where: "src", value: hit.trim() });
      }
    }
  }
  return out;
}

const findings = [...catalogFindings(), ...sourceFindings()];

describe("copy audit denylist (JUS-186)", () => {
  it("scans a non-trivial slice of the locale and source surface", () => {
    expect(catalogFindings().length).toBeGreaterThan(0);
    expect(Object.keys(sourceFiles).length).toBeGreaterThan(100);
  });

  it("flags no copy outside the allow-list", () => {
    const offenders = findings
      .filter((finding) => !(findingKey(finding) in ALLOWLIST))
      .map(
        (finding) =>
          `${finding.where} ${finding.id} [${finding.label}]: ${finding.value}`,
      );
    expect(offenders).toEqual([]);
  });

  it("keeps the allow-list honest (no stale entries)", () => {
    const matched = new Set(findings.map(findingKey));
    const stale = Object.keys(ALLOWLIST).filter((key) => !matched.has(key));
    expect(stale).toEqual([]);
  });
});
