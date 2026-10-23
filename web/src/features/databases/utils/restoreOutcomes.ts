import type { DatabaseRestore } from "@/features/databases/api/backups";

export interface RestoreOutcome {
  id: string;
  status: "completed" | "failed";
  error?: string;
}

export type RestoreStatusRow = Pick<DatabaseRestore, "id" | "status" | "error">;

export function advanceRestoreStatuses(
  seen: Map<string, string>,
  restores: RestoreStatusRow[],
): RestoreOutcome[] {
  const outcomes: RestoreOutcome[] = [];
  for (const restore of restores) {
    const previous = seen.get(restore.id);
    if (previous === "completed" || previous === "failed") {
      continue;
    }
    seen.set(restore.id, restore.status);
    if (previous !== "running") {
      continue;
    }
    if (restore.status === "completed" || restore.status === "failed") {
      outcomes.push({
        id: restore.id,
        status: restore.status,
        error: restore.error,
      });
    }
  }
  return outcomes;
}
