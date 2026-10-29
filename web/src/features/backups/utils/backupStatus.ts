import { i18n } from "@/shared/i18n";

import type {
  BackupRestore,
  BackupRun,
  ResourceType,
} from "@/features/backups/api/backups";

export function statusTagType(
  status: BackupRun["status"] | BackupRestore["status"],
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

export function resourceTypeLabel(type: ResourceType): string {
  const key = `backups.resourceTypes.${type}`;
  return i18n.global.te(key) ? String(i18n.global.t(key)) : type;
}

export function matchesBackupFilters(
  run: Pick<BackupRun, "resource_type" | "status">,
  type: "all" | ResourceType,
  status: "all" | BackupRun["status"],
): boolean {
  return (
    (type === "all" || run.resource_type === type) &&
    (status === "all" || run.status === status)
  );
}

export const CRON_PRESETS = ["0 2 * * *", "0 */6 * * *", "0 3 * * 0"] as const;

export function describeCron(cron: string): string {
  switch (cron.trim()) {
    case "0 2 * * *":
      return String(i18n.global.t("backups.cron.dailyAtTwo"));
    case "0 */6 * * *":
      return String(i18n.global.t("backups.cron.everySixHours"));
    case "0 3 * * 0":
      return String(i18n.global.t("backups.cron.weeklySunday"));
    default:
      return String(i18n.global.t("backups.cron.custom", { cron }));
  }
}

export function runDuration(
  startedAt: string,
  finishedAt: string | undefined,
): string {
  if (!finishedAt) {
    return String(i18n.global.t("backups.runs.inProgress"));
  }
  const seconds = Math.max(
    0,
    Math.round(
      (new Date(finishedAt).getTime() - new Date(startedAt).getTime()) / 1000,
    ),
  );
  if (!Number.isFinite(seconds)) {
    return "";
  }
  if (seconds < 60) {
    return String(i18n.global.t("backups.runs.seconds", { count: seconds }));
  }
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return rest === 0
    ? String(i18n.global.t("backups.runs.minutes", { count: minutes }))
    : String(
        i18n.global.t("backups.runs.minutesSeconds", {
          minutes,
          seconds: rest,
        }),
      );
}
