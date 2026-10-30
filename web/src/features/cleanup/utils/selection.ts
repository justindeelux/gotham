import type { CleanupTier } from "@/features/cleanup/api/cleanup";

export interface SelectableItem {
  key: string;
  id: string;
  serverId: string;
  category: string;
  tier: CleanupTier;
  name: string;
  sizeBytes: number;
  createdAt: string;
  reason: string;
}

export interface CategoryGroup {
  category: string;
  tier: CleanupTier;
  items: SelectableItem[];
  count: number;
  bytes: number;
}

export interface SelectionSummary {
  count: number;
  bytes: number;
  byNode: Record<string, { count: number; bytes: number }>;
  byCategory: Record<string, { count: number; bytes: number }>;
}

export interface NodeTotal {
  serverId: string;
  count: number;
  bytes: number;
}

const tierOrder: Record<CleanupTier, number> = {
  safe: 0,
  careful: 1,
  dangerous: 2,
};

export function itemKey(serverId: string, id: string): string {
  return `${serverId}::${id}`;
}

// Only the safe tier is preselected: careful items need a look and dangerous
// items are never removed without an explicit click and confirmation.
export function defaultSelection(items: SelectableItem[]): Set<string> {
  return new Set(
    items.filter((item) => item.tier === "safe").map((item) => item.key),
  );
}

export function toggleItem(
  selected: Set<string>,
  key: string,
  checked: boolean,
): Set<string> {
  const next = new Set(selected);
  if (checked) {
    next.add(key);
  } else {
    next.delete(key);
  }
  return next;
}

export function setCategorySelection(
  items: SelectableItem[],
  selected: Set<string>,
  category: string,
  select: boolean,
): Set<string> {
  const next = new Set(selected);
  for (const item of items) {
    if (item.category !== category) {
      continue;
    }
    if (select) {
      if (item.tier !== "dangerous") {
        next.add(item.key);
      }
    } else {
      next.delete(item.key);
    }
  }
  return next;
}

export function categoryGroups(items: SelectableItem[]): CategoryGroup[] {
  const groups = new Map<string, CategoryGroup>();
  for (const item of items) {
    let group = groups.get(item.category);
    if (!group) {
      group = {
        category: item.category,
        tier: item.tier,
        items: [],
        count: 0,
        bytes: 0,
      };
      groups.set(item.category, group);
    }
    group.items.push(item);
    group.count += 1;
    group.bytes += item.sizeBytes;
  }
  return [...groups.values()].sort(
    (left, right) =>
      tierOrder[left.tier] - tierOrder[right.tier] ||
      left.category.localeCompare(right.category),
  );
}

export function selectionSummary(
  items: SelectableItem[],
  selected: Set<string>,
): SelectionSummary {
  const summary: SelectionSummary = {
    count: 0,
    bytes: 0,
    byNode: {},
    byCategory: {},
  };
  for (const item of items) {
    if (!selected.has(item.key)) {
      continue;
    }
    summary.count += 1;
    summary.bytes += item.sizeBytes;
    const node = (summary.byNode[item.serverId] ??= { count: 0, bytes: 0 });
    node.count += 1;
    node.bytes += item.sizeBytes;
    const category = (summary.byCategory[item.category] ??= {
      count: 0,
      bytes: 0,
    });
    category.count += 1;
    category.bytes += item.sizeBytes;
  }
  return summary;
}

export function totals(items: SelectableItem[]): { count: number; bytes: number } {
  let count = 0;
  let bytes = 0;
  for (const item of items) {
    count += 1;
    bytes += item.sizeBytes;
  }
  return { count, bytes };
}
