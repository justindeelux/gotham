import type { DatabaseRestore } from "../api/backups";

/** A terminal restore outcome worth announcing to the user. */
export interface RestoreOutcome {
  id: string;
  status: "completed" | "failed";
  error?: string;
}

/** The status fields the transition needs from one restore row. */
export type RestoreStatusRow = Pick<DatabaseRestore, "id" | "status" | "error">;

/**
 * advanceRestoreStatuses folds the latest restore list into the seen-status map
 * and returns the outcomes to announce. A restore is announced once, when it
 * moves from running to completed/failed.
 *
 * Status only moves forward: a stale list that still reports a terminal restore
 * as running cannot revert the seen status, so it cannot re-arm an announcement
 * that a newer poll already made.
 */
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
