import type { DatabaseBackup } from "@/features/databases";
import type { Database } from "@/features/databases";

/**
 * mergeBackupsById folds a server list into the cached one by id and sorts the
 * result newest first. A server row wins for a known id; a local `running` row
 * the server has not reported yet is kept — dropping it would stop the page's
 * running-job poll — and sorting keeps the just-queued row at the top instead
 * of pushing it below the older server rows.
 */
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

/**
 * mergeDatabasesById folds a server list into the local one. A server row wins
 * for a known id and a local-only row is preserved, but an id in deletedIds is
 * dropped from both sides: a list response that started before a delete
 * resolved after it, so without this it would resurrect the removed row.
 */
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
