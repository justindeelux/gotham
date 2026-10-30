import { describe, expect, it } from "vitest";

import en from "@/features/cleanup/locales/en";
import vi from "@/features/cleanup/locales/vi";
import { cleanupScanResponseSchema } from "@/features/cleanup/schemas/cleanup";
import {
  categoryGroups,
  defaultSelection,
  itemKey,
  selectionSummary,
  setCategorySelection,
  toggleItem,
  totals,
} from "@/features/cleanup/utils/selection";
import type { SelectableItem } from "@/features/cleanup/utils/selection";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import { parseWith } from "@/shared/validation/parse";

function item(
  id: string,
  overrides: Partial<SelectableItem> = {},
): SelectableItem {
  return {
    key: itemKey("node-1", id),
    id,
    serverId: "node-1",
    category: "build_cache",
    tier: "safe",
    name: id,
    sizeBytes: 10,
    createdAt: "2026-10-01T00:00:00Z",
    reason: "",
    ...overrides,
  };
}

const items = [
  item("cache", { sizeBytes: 100 }),
  item("dangling", { category: "dangling_images", sizeBytes: 50 }),
  item("unused", { category: "unused_images", tier: "careful", sizeBytes: 200 }),
  item("volume", {
    serverId: "node-2",
    key: itemKey("node-2", "volume"),
    category: "orphaned_volumes",
    tier: "dangerous",
    sizeBytes: 400,
  }),
];

describe("cleanup selection", () => {
  it("preselects only safe items", () => {
    const selected = defaultSelection(items);
    expect([...selected].sort()).toEqual([
      itemKey("node-1", "cache"),
      itemKey("node-1", "dangling"),
    ]);
    expect(selected.has(itemKey("node-1", "unused"))).toBe(false);
    expect(selected.has(itemKey("node-2", "volume"))).toBe(false);
  });

  it("selects and clears a whole category without ever adding dangerous items", () => {
    const safe = setCategorySelection(items, new Set(), "build_cache", true);
    expect(safe.has(itemKey("node-1", "cache"))).toBe(true);

    const all = setCategorySelection(items, new Set(), "orphaned_volumes", true);
    expect(all.size).toBe(0);

    const cleared = setCategorySelection(items, defaultSelection(items), "dangling_images", false);
    expect(cleared.has(itemKey("node-1", "dangling"))).toBe(false);
    expect(cleared.has(itemKey("node-1", "cache"))).toBe(true);
  });

  it("toggles single items in and out", () => {
    const checked = toggleItem(new Set(), itemKey("node-2", "volume"), true);
    expect(checked.has(itemKey("node-2", "volume"))).toBe(true);
    const unchecked = toggleItem(checked, itemKey("node-2", "volume"), false);
    expect(unchecked.has(itemKey("node-2", "volume"))).toBe(false);
  });

  it("summarizes the selected count, bytes, nodes and categories", () => {
    const selected = new Set([
      itemKey("node-1", "cache"),
      itemKey("node-2", "volume"),
    ]);
    const summary = selectionSummary(items, selected);
    expect(summary.count).toBe(2);
    expect(summary.bytes).toBe(500);
    expect(summary.byNode["node-1"]).toEqual({ count: 1, bytes: 100 });
    expect(summary.byNode["node-2"]).toEqual({ count: 1, bytes: 400 });
    expect(summary.byCategory["build_cache"]).toEqual({ count: 1, bytes: 100 });
    expect(summary.byCategory["orphaned_volumes"]).toEqual({ count: 1, bytes: 400 });
  });

  it("groups by category in tier order with per-category totals", () => {
    const groups = categoryGroups(items);
    expect(groups.map((group) => group.category)).toEqual([
      "build_cache",
      "dangling_images",
      "unused_images",
      "orphaned_volumes",
    ]);
    expect(groups[0]).toMatchObject({ tier: "safe", count: 1, bytes: 100 });
    expect(totals(items)).toEqual({ count: 4, bytes: 750 });
  });
});

describe("cleanup response schema", () => {
  it("parses nodes with items and notices", () => {
    const payload = {
      nodes: [
        {
          server_id: "node-1",
          status: "ok",
          reason: "",
          items: [
            {
              id: "image:sha256:abc",
              category: "dangling_images",
              tier: "safe",
              name: "abc",
              size_bytes: 42,
              created_at: "2026-10-01T00:00:00Z",
              reason: "dangling",
            },
          ],
          notices: [{ category: "journal", reason: "unsupported" }],
        },
      ],
    };
    expect(cleanupScanResponseSchema.safeParse(payload).success).toBe(true);
    const parsed = parseWith(cleanupScanResponseSchema, payload, { strict: true });
    expect(parsed.nodes[0]?.notices).toEqual([
      { category: "journal", reason: "unsupported" },
    ]);
  });

  it("accepts a skipped node with empty arrays", () => {
    const payload = {
      nodes: [
        {
          server_id: "node-1",
          status: "skipped",
          reason: "busy",
          items: [],
          notices: [],
        },
      ],
    };
    expect(cleanupScanResponseSchema.safeParse(payload).success).toBe(true);
  });
});

describe("cleanup catalogs", () => {
  it("keeps en and vi in parity", () => {
    expect(checkCatalogParity(en, vi)).toEqual([]);
  });
});
