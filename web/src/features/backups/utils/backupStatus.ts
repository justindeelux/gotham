import { activeLocale, i18n } from "@/shared/i18n";

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

const CRON_FIELD_BOUNDS: ReadonlyArray<readonly [number, number]> = [
  [0, 59],
  [0, 23],
  [1, 31],
  [1, 12],
  [0, 7],
];

interface CronField {
  values: Set<number>;
  any: boolean;
  step: number | null;
}

function cronText(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

function parseCronNumber(value: string): number | null {
  return /^\d+$/.test(value) ? Number(value) : null;
}

function parseCronField(part: string, min: number, max: number): CronField | null {
  const values = new Set<number>();
  let any = false;
  let step: number | null = null;
  for (const item of part.split(",")) {
    if (item === "") {
      return null;
    }
    const slash = item.indexOf("/");
    const rangePart = slash === -1 ? item : item.slice(0, slash);
    const stepPart = slash === -1 ? "" : item.slice(slash + 1);
    let fieldStep = 1;
    if (slash !== -1) {
      const parsed = parseCronNumber(stepPart);
      if (parsed === null || parsed === 0) {
        return null;
      }
      fieldStep = parsed;
    }
    let low: number | null;
    let high: number | null;
    if (rangePart === "*") {
      any = any || slash === -1;
      step = slash === -1 ? step : fieldStep;
      low = min;
      high = max;
    } else if (rangePart.includes("-")) {
      const bounds = rangePart.split("-");
      low = parseCronNumber(bounds[0] ?? "");
      high = parseCronNumber(bounds[1] ?? "");
    } else {
      low = parseCronNumber(rangePart);
      high = slash === -1 ? low : max;
    }
    if (low === null || high === null || low < min || high > max || low > high) {
      return null;
    }
    for (let value = low; value <= high; value += fieldStep) {
      values.add(max === 7 && value === 7 ? 0 : value);
    }
  }
  if (part.includes(",")) {
    step = null;
  }
  return values.size === 0 ? null : { values, any, step };
}

function parseCron(cron: string): CronField[] | null {
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) {
    return null;
  }
  const fields: CronField[] = [];
  for (const [index, part] of parts.entries()) {
    const bounds = CRON_FIELD_BOUNDS[index]!;
    const field = parseCronField(part, bounds[0], bounds[1]);
    if (field === null) {
      return null;
    }
    fields.push(field);
  }
  return fields;
}

function singleValue(field: CronField): number | null {
  return field.values.size === 1 ? [...field.values][0]! : null;
}

function pad(value: number): string {
  return value.toString().padStart(2, "0");
}

function weekdayName(weekday: number): string {
  // 2024-01-07 is a Sunday, so offsetting by the weekday index is stable.
  const date = new Date(Date.UTC(2024, 0, 7 + weekday));
  const locale = activeLocale.value === "vi" ? "vi-VN" : "en-US";
  return new Intl.DateTimeFormat(locale, { weekday: "long", timeZone: "UTC" })
    .format(date);
}

function cronSentence(fields: CronField[]): string {
  const [minute, hour, dayOfMonth, month, weekday] = fields as [
    CronField,
    CronField,
    CronField,
    CronField,
    CronField,
  ];
  const daysAny = dayOfMonth.any && month.any;
  const minutes = singleValue(minute);
  const hours = singleValue(hour);

  if (minute.any && hour.any && daysAny && weekday.any) {
    return cronText("backups.cron.everyMinute");
  }
  if (minute.step !== null && hour.any && daysAny && weekday.any) {
    return cronText("backups.cron.everyMinutes", { count: minute.step });
  }
  if (minutes !== null && hour.any && daysAny && weekday.any) {
    return cronText("backups.cron.hourlyAt", { minute: pad(minutes) });
  }
  if (minutes !== null && hour.step !== null && daysAny && weekday.any) {
    return cronText("backups.cron.everyNHours", {
      count: hour.step,
      minute: pad(minutes),
    });
  }
  if (minutes === null || hours === null) {
    return cronText("backups.cron.custom");
  }
  const time = `${pad(hours)}:${pad(minutes)}`;
  if (daysAny && weekday.any) {
    return cronText("backups.cron.dailyAt", { time });
  }
  if (daysAny && !weekday.any) {
    const weekdays = [...weekday.values].sort((a, b) => a - b).join(",");
    if (weekdays === "1,2,3,4,5") {
      return cronText("backups.cron.weekdaysAt", { time });
    }
    if (weekday.values.size === 1) {
      return cronText("backups.cron.weeklyAt", {
        day: weekdayName([...weekday.values][0]!),
        time,
      });
    }
  }
  if (month.any && weekday.any && singleValue(dayOfMonth) !== null) {
    return cronText("backups.cron.monthlyAt", {
      day: singleValue(dayOfMonth)!,
      time,
    });
  }
  return cronText("backups.cron.custom");
}

export interface DescribeCronOptions {
  /** Return only the plain-language sentence: no expression and no time note. */
  short?: boolean;
}

// The backend runs the schedule in the server's local time, not UTC, so the
// full sentence carries that note wherever it is shown. The short form is for
// places that already show the expression next to it.
export function describeCron(
  cron: string,
  options: DescribeCronOptions = {},
): string {
  void activeLocale.value;
  const fields = parseCron(cron);
  if (fields === null) {
    return "";
  }
  const sentence = cronSentence(fields);
  if (options.short === true) {
    return sentence;
  }
  return `${sentence} (${cron.trim()}) · ${cronText("backups.cron.serverTime")}`;
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
