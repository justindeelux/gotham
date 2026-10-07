import { i18n } from "@/shared/i18n";
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

/**
 * runDisplay renders a wire run status/type through the catalog when it is a
 * known value, and falls back to the raw wire string otherwise (same pattern
 * as DatabaseStatusTag): an unknown future value never renders a message key.
 */
export function runDisplay(
  namespace: "runStatus" | "runType",
  value: string,
): string {
  const key = `databases.backups.${namespace}.${value}`;
  return i18n.global.te(key) ? String(i18n.global.t(key)) : value;
}
