import { i18n } from "@/shared/i18n";
import type {
  DatabaseBackup,
  DatabaseRestore,
} from "@/features/databases/api/backups";

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

export function runDisplay(
  namespace: "runStatus" | "runType",
  value: string,
): string {
  const key = `databases.backups.${namespace}.${value}`;
  return i18n.global.te(key) ? String(i18n.global.t(key)) : value;
}
