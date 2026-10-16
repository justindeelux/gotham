import { describe, expect, it } from "vitest";

import { leafEntries } from "@/shared/i18n/catalog";
import enCommon from "@/shared/i18n/locales/en";
import viCommon from "@/shared/i18n/locales/vi";
import editorEn from "@/features/files/locales/editorEn";
import editorVi from "@/features/files/locales/editorVi";

/**
 * JUS-186: user-visible copy must never send a deployed instance to the
 * repository (file paths, docs) or to a shell it cannot run, and must not leak
 * an operator's env-var kill switch. These tokens mark copy that breaks that
 * rule; each surviving match has to be allow-listed with a reason below, so a
 * new one fails the build instead of shipping.
 */
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

/**
 * Allow-listed copy, keyed by "<module>.<dotted.key>", with the reason it may
 * keep a denylisted token. Keep this list explicit and tiny: it is the whole
 * record of where the rule is consciously waived.
 */
const ALLOWLIST: Record<string, string> = {
  "instance-settings.unsupported":
    "Handled by JUS-185 (instance-settings Network message); that parallel change owns this string, not this one.",
  "servers.wizard.installHowto":
    "The install wizard's one-time manual agent bootstrap, kept as the explicit alternative; it names the installer's own env var on purpose.",
};

interface CopyEntry {
  id: string;
  locale: "en" | "vi";
  value: string;
}

/** collectCopy flattens every discovered locale catalog to leaf strings. */
function collectCopy(): CopyEntry[] {
  const out: CopyEntry[] = [];
  const add = (
    module: string,
    catalog: Record<string, unknown>,
    locale: "en" | "vi",
  ): void => {
    for (const [key, value] of leafEntries(catalog)) {
      if (typeof value === "string") {
        out.push({ id: `${module}.${key}`, locale, value });
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

  // Shared catalogs (namespace `common`) plus the split-out files-editor
  // catalogs (lazy `files.editor`, not part of the eager glob scan).
  add("common", enCommon as Record<string, unknown>, "en");
  add("common", viCommon as Record<string, unknown>, "vi");
  add("files.editor", editorEn as unknown as Record<string, unknown>, "en");
  add("files.editor", editorVi as unknown as Record<string, unknown>, "vi");

  return out;
}

const copy = collectCopy();

/** violations returns every leaf string that matches a denylist token. */
function violations(): Array<CopyEntry & { label: string }> {
  const hits: Array<CopyEntry & { label: string }> = [];
  for (const entry of copy) {
    for (const rule of DENYLIST) {
      if (rule.pattern.test(entry.value)) {
        hits.push({ ...entry, label: rule.label });
      }
    }
  }
  return hits;
}

describe("copy audit denylist (JUS-186)", () => {
  it("scans a non-trivial slice of the locale surface", () => {
    expect(copy.length).toBeGreaterThan(500);
  });

  it("flags no copy outside the allow-list", () => {
    const offenders = violations()
      .filter((hit) => !(hit.id in ALLOWLIST))
      .map((hit) => `${hit.locale} ${hit.id} [${hit.label}]: ${hit.value}`);
    expect(offenders).toEqual([]);
  });

  it("keeps the allow-list honest (no stale entries)", () => {
    const matched = new Set(violations().map((hit) => hit.id));
    const stale = Object.keys(ALLOWLIST).filter((id) => !matched.has(id));
    expect(stale).toEqual([]);
  });
});
