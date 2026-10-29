import type { BackupRun } from "@/features/backups/api/backups";

export function mergeBackupsById(
  existing: BackupRun[],
  incoming: BackupRun[],
): BackupRun[] {
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
