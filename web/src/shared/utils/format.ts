import { activeLocale, localeTag } from "@/shared/i18n/locale";
import type { Locale } from "@/shared/i18n/locale";
import enCatalog from "@/shared/i18n/locales/en";
import viCatalog from "@/shared/i18n/locales/vi";

const emptyPlaceholder = "—";

export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || Number.isNaN(bytes)) {
    return emptyPlaceholder;
  }
  if (bytes <= 0) {
    return "0 B";
  }

  const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  const exponent = Math.max(
    0,
    Math.min(
      Math.floor(Math.log(bytes) / Math.log(1024)),
      units.length - 1,
    ),
  );
  const value = bytes / 1024 ** exponent;

  let decimals = 0;
  if (exponent > 0) {
    decimals = value >= 100 ? 1 : 2;
  }

  return `${value.toFixed(decimals)} ${units[exponent]}`;
}

export function formatPercent(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return emptyPlaceholder;
  }
  const clamped = Math.min(Math.max(value, 0), 100);
  return `${clamped.toFixed(1)}%`;
}

export const USAGE_WARN_PERCENT = 60;

export const USAGE_DANGER_PERCENT = 80;

export type UsageLevel = "ok" | "warn" | "danger";

export function usageLevel(percent: number): UsageLevel {
  if (percent >= USAGE_DANGER_PERCENT) {
    return "danger";
  }
  if (percent >= USAGE_WARN_PERCENT) {
    return "warn";
  }
  return "ok";
}

export function usageBarColor(level: UsageLevel, healthy: string): string {
  switch (level) {
    case "danger":
      return "var(--danger)";
    case "warn":
      return "var(--warn)";
    default:
      return healthy;
  }
}

export interface UsageView {
  label: string;
  color: string;
  percentage: number;
}

export function usageView(
  value: number | null | undefined,
  healthy: string,
): UsageView {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return { label: "—", color: healthy, percentage: 0 };
  }
  const percentage = toPercent(value);
  return {
    label: `${percentage}%`,
    color: usageBarColor(usageLevel(percentage), healthy),
    percentage,
  };
}

export function toPercent(value: number | null | undefined): number {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return 0;
  }
  const scaled = value <= 1 ? value * 100 : value;
  return Math.round(Math.min(Math.max(scaled, 0), 100));
}

const rtfCache = new Map<string, Intl.RelativeTimeFormat>();

function rtf(tag: string, numeric: "always" | "auto"): Intl.RelativeTimeFormat {
  const key = `${tag}:${numeric}`;
  let formatter = rtfCache.get(key);
  if (!formatter) {
    formatter = new Intl.RelativeTimeFormat(tag, { numeric });
    rtfCache.set(key, formatter);
  }
  return formatter;
}

export interface RelativeTimeOptions {
  long?: boolean;
  now?: number;
}

export function relativeTime(
  iso: string | null | undefined,
  locale?: Locale | null,
  options: RelativeTimeOptions = {},
): string {
  const loc = locale ?? activeLocale.value;
  const time = loc === "vi" ? viCatalog.time : enCatalog.time;
  if (!iso) {
    return time.never;
  }

  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return time.unknown;
  }

  const deltaSeconds = Math.round(((options.now ?? Date.now()) - then) / 1000);
  const future = deltaSeconds < 0;
  const seconds = Math.abs(deltaSeconds);

  if (options.long) {
    if (seconds < 60) {
      return time.justNow;
    }
    const longUnits: Array<[Intl.RelativeTimeFormatUnit, number]> = [
      ["year", 31536000],
      ["month", 2592000],
      ["day", 86400],
      ["hour", 3600],
      ["minute", 60],
    ];
    const [unit, span] =
      longUnits.find(([, size]) => seconds >= size) ?? ["minute", 60];
    const value = Math.round(seconds / span);
    return rtf(localeTag(loc), "auto").format(future ? value : -value, unit);
  }

  if (loc === "en") {
    const unit = (value: number, suffix: string): string =>
      future ? `in ${value}${suffix}` : `${value}${suffix} ago`;

    if (seconds < 45) {
      return future ? "in a moment" : "just now";
    }

    const minutes = Math.round(seconds / 60);
    if (minutes < 60) {
      return unit(minutes, "m");
    }

    const hours = Math.round(minutes / 60);
    if (hours < 24) {
      return unit(hours, "h");
    }

    const days = Math.round(hours / 24);
    if (days < 30) {
      return unit(days, "d");
    }

    const months = Math.round(days / 30);
    if (months < 12) {
      return unit(months, "mo");
    }

    return unit(Math.round(months / 12), "y");
  }

  if (seconds < 45) {
    return future ? time.inMoment : time.justNow;
  }
  const relative = rtf("vi-VN", "always");
  const signed = (
    value: number,
    unit: "minute" | "hour" | "day" | "month" | "year",
  ): string => relative.format(future ? value : -value, unit);

  const minutes = Math.round(seconds / 60);
  if (minutes < 60) {
    return signed(minutes, "minute");
  }

  const hours = Math.round(minutes / 60);
  if (hours < 24) {
    return signed(hours, "hour");
  }

  const days = Math.round(hours / 24);
  if (days < 30) {
    return signed(days, "day");
  }

  const months = Math.round(days / 30);
  if (months < 12) {
    return signed(months, "month");
  }

  return signed(Math.round(months / 12), "year");
}

export function formatDate(
  iso: string | null | undefined,
  locale?: Locale | null,
): string {
  const loc = locale ?? activeLocale.value;
  if (!iso) {
    return emptyPlaceholder;
  }
  const time = new Date(iso);
  if (Number.isNaN(time.getTime())) {
    return (loc === "vi" ? viCatalog : enCatalog).time.unknown;
  }
  return time.toLocaleDateString(localeTag(loc), {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}

export function formatDateTime(
  iso: string | null | undefined,
  locale?: Locale | null,
): string {
  const loc = locale ?? activeLocale.value;
  if (!iso) {
    return emptyPlaceholder;
  }
  const time = new Date(iso);
  if (Number.isNaN(time.getTime())) {
    return (loc === "vi" ? viCatalog : enCatalog).time.unknown;
  }
  return time.toLocaleString(localeTag(loc), {
    dateStyle: "medium",
    timeStyle: "short",
  });
}

export function expiryLabel(
  iso: string | null | undefined,
  locale?: Locale | null,
): string {
  const loc = locale ?? activeLocale.value;
  const time = loc === "vi" ? viCatalog.time : enCatalog.time;
  if (!iso) {
    return emptyPlaceholder;
  }
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return time.unknown;
  }

  const now = Date.now();
  const days = Math.round((then - now) / 86_400_000);
  if (days > 0) {
    return loc === "vi"
      ? `hết hạn sau ${days} ngày`
      : `expires in ${days} day${days === 1 ? "" : "s"}`;
  }
  if (days < 0) {
    const overdue = -days;
    return loc === "vi"
      ? `đã hết hạn ${overdue} ngày trước`
      : `expired ${overdue} day${overdue === 1 ? "" : "s"} ago`;
  }
  if (then >= now) {
    return time.expiresToday;
  }
  return time.expiredToday;
}
