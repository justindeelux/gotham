import type { Database } from "@/features/databases/api/databases";

export function mergeDatabasesById(
  server: Database[],
  local: Database[],
  deletedIds: ReadonlySet<string> = new Set(),
): Database[] {
  const byId = new Map<string, Database>();
  for (const item of server) {
    if (!deletedIds.has(item.id)) {
      byId.set(item.id, item);
    }
  }
  for (const item of local) {
    if (!deletedIds.has(item.id) && !byId.has(item.id)) {
      byId.set(item.id, item);
    }
  }
  return [...byId.values()];
}
