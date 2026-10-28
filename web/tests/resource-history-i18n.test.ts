import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import en from "../src/features/monitoring/locales/en";
import vi from "../src/features/monitoring/locales/vi";

const panelSource = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), "../src/features/monitoring/components/ResourceHistoryPanel.vue"),
  "utf8",
);

function lookup(catalog: Record<string, unknown>, key: string): unknown {
  // Feature catalogs are registered under their namespace at runtime, but the
  // raw module is the namespace body.
  return key.replace(/^monitoring\./, "").split(".").reduce<unknown>((value, part) => {
    if (typeof value !== "object" || value === null) {
      return undefined;
    }
    return (value as Record<string, unknown>)[part];
  }, catalog);
}

function usedKeys(): string[] {
  const keys = new Set<string>();
  for (const match of panelSource.matchAll(/\bt\(\s*["']([^"']+)["']/g)) {
    if (match[1]?.startsWith("monitoring.resourceHistory.")) {
      keys.add(match[1]);
    }
  }
  // The panel builds the chart title from this dynamic prefix.
  for (const chart of ["cpu", "memory", "network", "disk"]) {
    keys.add(`monitoring.resourceHistory.charts.${chart}`);
  }
  return Array.from(keys).sort();
}

describe("resource history i18n", () => {
  it("defines every key the Metrics panel renders in EN and VI", () => {
    const keys = usedKeys();
    expect(keys.length).toBeGreaterThan(10);
    for (const catalog of [en, vi]) {
      for (const key of keys) {
        expect(lookup(catalog, key), `missing ${key}`).toBeTypeOf("string");
      }
    }
  });

  it("does not reference the removed pointCount key", () => {
    expect(panelSource).not.toContain("monitoring.resourceHistory.pointCount");
  });
});
