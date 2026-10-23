import type { DatabaseBackup } from "@/features/databases/api/backups";
import type { Database } from "@/features/databases/api/databases";

export function mergeBackupsById(
  existing: DatabaseBackup[],
  incoming: DatabaseBackup[],
): DatabaseBackup[] {
  const byId = new Map(incoming.map((item) => [item.id, item]));
  for (const item of existing) {
    if (item.status === "running" && !byId.has(item.id)) {
      byId.set(item.id, item);
    }
  }
  return [...byId.values()].sort((a, b) =>
    b.created_at.localeCompare(a.created_at),
  );
}

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
