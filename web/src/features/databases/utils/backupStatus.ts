import type {
  DatabaseBackup,
  DatabaseRestore,
} from "@/features/databases/api/backups";

/** statusTagType maps a backup or restore status to a Naive UI tag type. */
export function statusTagType(
  status: DatabaseBackup["status"] | DatabaseRestore["status"],
): "success" | "warning" | "error" {
  if (status === "completed") {
    return "success";
  }
  if (status === "failed") {
    return "error";
  }
  return "warning";
}
