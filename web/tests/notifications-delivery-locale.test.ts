import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import en from "@/features/notifications/locales/en";
import vi from "@/features/notifications/locales/vi";

const components = [
  "src/features/notifications/components/ChannelCard.vue",
  "src/features/notifications/components/DeliveryFailuresList.vue",
  "src/features/incidents/components/NeedsAttentionSection.vue",
];

function readKey(file: string): string[] {
  const source = readFileSync(file, "utf8");
  const keys = new Set<string>();
  for (const match of source.matchAll(/\bt\("([^"]+)"/g)) {
    keys.add(match[1]);
  }
  for (const match of source.matchAll(/\$t\("([^"]+)"/g)) {
    keys.add(match[1]);
  }
  return [...keys];
}

function lookup(source: Record<string, unknown>, key: string): unknown {
  return key.split(".").reduce<unknown>((node, part) => {
    if (node && typeof node === "object") {
      return (node as Record<string, unknown>)[part];
    }
    return undefined;
  }, source);
}

describe("notification delivery locales", () => {
  it("defines every t() key used by the delivery components in both locales", () => {
    const locales: Record<string, Record<string, unknown>> = {
      en: { notifications: en },
      vi: { notifications: vi },
    };
    for (const file of components) {
      for (const key of readKey(file)) {
        if (!key.startsWith("notifications.")) {
          continue;
        }
        for (const [name, locale] of Object.entries(locales)) {
          expect(
            lookup(locale, key),
            `${name} is missing ${key} used by ${file}`,
          ).toBeTypeOf("string");
        }
      }
    }
  });
});
