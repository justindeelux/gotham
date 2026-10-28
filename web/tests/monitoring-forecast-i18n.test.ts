import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import incidentsEn from "../src/features/incidents/locales/en";
import incidentsVi from "../src/features/incidents/locales/vi";
import en from "../src/features/monitoring/locales/en";
import vi from "../src/features/monitoring/locales/vi";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function lookup(catalog: Record<string, unknown>, key: string): unknown {
  return key
    .replace(/^(monitoring|incidents)\./, "")
    .split(".")
    .reduce<unknown>((value, part) => {
      if (typeof value !== "object" || value === null) {
        return undefined;
      }
      return (value as Record<string, unknown>)[part];
    }, catalog);
}

function staticKeys(source: string, prefix: string): string[] {
  const keys = new Set<string>();
  for (const match of source.matchAll(/\bt\(\s*["']([^"']+)["']/g)) {
    if (match[1]?.startsWith(prefix)) {
      keys.add(match[1]);
    }
  }
  return Array.from(keys).sort();
}

describe("filesystem forecast i18n", () => {
  const components = [
    "src/features/monitoring/components/ServerPressureCard.vue",
    "src/features/monitoring/pages/MonitoringSettingsPage.vue",
  ];

  it("defines every monitoring key the forecast UI renders in EN and VI", () => {
    const keys = new Set<string>();
    for (const component of components) {
      const source = readFileSync(resolve(root, component), "utf8");
      for (const key of staticKeys(source, "monitoring.")) {
        keys.add(key);
      }
    }
    // The pressure card builds the forecast phrase from this status mapping.
    for (const status of [
      "estimated",
      "nonGrowing",
      "insufficient",
      "stale",
      "lowConfidence",
      "unavailable",
      "detail",
    ]) {
      keys.add(`monitoring.forecast.${status}`);
    }
    keys.add("monitoring.detectors.forecast");
    keys.add("monitoring.rules.forecast");
    keys.add("monitoring.rules.forecastHint");

    expect(keys.size).toBeGreaterThan(5);
    for (const catalog of [en, vi]) {
      for (const key of keys) {
        expect(lookup(catalog, key), `missing ${key}`).toBeTypeOf("string");
      }
    }
  });

  it("defines the forecast incident reason and grouped hold in EN and VI", () => {
    for (const catalog of [incidentsEn, incidentsVi]) {
      expect(lookup(catalog, "incidents.reasons.forecast")).toBeTypeOf("string");
      expect(lookup(catalog, "incidents.hold.groupedDisk")).toBeTypeOf("string");
    }
  });
});
