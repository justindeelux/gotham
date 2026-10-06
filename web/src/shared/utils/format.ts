/**
 * Small display formatters shared by the server UI. Deliberately dependency-free
 * so the bundle stays lean and the helpers are trivial to unit-test later.
 * Date/time wording follows the active UI locale; byte units, thresholds,
 * null placeholders and invalid-date handling are locale-independent.
 */
import { localeTag } from "@/shared/i18n/locale";
import type { Locale } from "@/shared/i18n/locale";

/** Placeholder shown when a value is missing. */
const emptyPlaceholder = "—";

/** formatBytes renders a byte count using binary (KiB/MiB/GiB) units. */
export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || Number.isNaN(bytes)) {
    return emptyPlaceholder;
  }
  if (bytes <= 0) {
    return "0 B";
  }

  const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  const exponent = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  );
  const value = bytes / 1024 ** exponent;

  let decimals = 0;
  if (exponent > 0) {
    decimals = value >= 100 ? 1 : 2;
  }

  return `${value.toFixed(decimals)} ${units[exponent]}`;
}

/** formatPercent renders a 0..100 float as a rounded percentage string. */
export function formatPercent(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return emptyPlaceholder;
  }
  const clamped = Math.min(Math.max(value, 0), 100);
  return `${clamped.toFixed(1)}%`;
}

/**
 * Shared usage-meter thresholds (percent, 0..100 after toPercent).
 *
 *   ok      below 60 — healthy headroom, rendered green / base color.
 *   warn    60..79   — getting full, rendered amber.
 *   danger  80+      — act soon, rendered red.
 *
 * The dashboard and the server list share this helper so the same reading
 * never shows two severities on two pages.
 */
export const USAGE_WARN_PERCENT = 60;

/**
 * USAGE_DANGER_PERCENT is the single danger threshold for CPU/RAM/disk meters.
 * Kept here so the dashboard and the server list cannot drift apart (B4-13).
 */
export const USAGE_DANGER_PERCENT = 80;

/** Severity level of a normalized 0..100 usage reading. */
export type UsageLevel = "ok" | "warn" | "danger";

/** usageLevel maps a normalized percentage onto its severity level. */
export function usageLevel(percent: number): UsageLevel {
  if (percent >= USAGE_DANGER_PERCENT) {
    return "danger";
  }
  if (percent >= USAGE_WARN_PERCENT) {
    return "warn";
  }
  return "ok";
}

/**
 * usageBarColor maps a severity level onto its bar color: the metric keeps
 * its healthy base hue (per the dashboard/servers mockups CPU renders accent
 * while RAM/disk render success green), warn renders amber and danger red on
 * every page. The dashboard and the server list share this helper so the same
 * reading never shows two severities on two pages.
 */
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

/** UsageView is one rendered usage tile (label, bar color, bar percent). */
export interface UsageView {
  /** label is the displayed reading, or an em dash when there is none. */
  label: string;
  /** color is the bar color from the shared severity scale. */
  color: string;
  /** percentage is the normalized 0..100 reading the bar renders. */
  percentage: number;
}

/**
 * usageView normalizes a heartbeat usage reading for display. Null,
 * undefined and NaN render as an em dash (never as a false 0%); other values
 * are clamped to 0..100 through toPercent before the shared thresholds apply.
 */
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

/** toPercent normalizes a usage reading to a 0-100 percentage.
 *
 * The proto contract (proto/agent/v1/agent.proto) defines heartbeat usage
 * as a fraction 0..1, so fractional readings are scaled up. Readings above
 * 1 pass through unchanged for forward compatibility.
 */
export function toPercent(value: number | null | undefined): number {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return 0;
  }
  const scaled = value <= 1 ? value * 100 : value;
  return Math.round(Math.min(Math.max(scaled, 0), 100));
}

/** relativeTime renders an ISO timestamp as a short "x ago" / "in x" string. */
export function relativeTime(
  iso: string | null | undefined,
  locale?: Locale | null,
): string {
  const tag = localeTag(locale);
  const vietnamese = tag === "vi-VN";
  if (!iso) {
    return vietnamese ? "không bao giờ" : "never";
  }

  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return vietnamese ? "không rõ" : "unknown";
  }

  const deltaSeconds = Math.round((Date.now() - then) / 1000);
  const future = deltaSeconds < 0;
  const seconds = Math.abs(deltaSeconds);

  /** unit renders the magnitude with the direction that fits the timestamp. */
  const unit = (value: number, suffix: string): string =>
    vietnamese
      ? future
        ? `sau ${value}${suffix}`
        : `${value}${suffix} trước`
      : future
        ? `in ${value}${suffix}`
        : `${value}${suffix} ago`;

  if (seconds < 45) {
    if (future) {
      return vietnamese ? "sắp tới" : "in a moment";
    }
    return vietnamese ? "vừa xong" : "just now";
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

/** formatDate renders an ISO timestamp as a short absolute date ("28 Sep 2026"). */
export function formatDate(
  iso: string | null | undefined,
  locale?: Locale | null,
): string {
  const tag = localeTag(locale);
  if (!iso) {
    return emptyPlaceholder;
  }
  const time = new Date(iso);
  if (Number.isNaN(time.getTime())) {
    return tag === "vi-VN" ? "không rõ" : "unknown";
  }
  return time.toLocaleDateString(tag, {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}

/** expiryLabel renders how far away (or past) an expiry timestamp is. */
export function expiryLabel(
  iso: string | null | undefined,
  locale?: Locale | null,
): string {
  const tag = localeTag(locale);
  const vietnamese = tag === "vi-VN";
  if (!iso) {
    return emptyPlaceholder;
  }
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return vietnamese ? "không rõ" : "unknown";
  }

  const now = Date.now();
  const days = Math.round((then - now) / 86_400_000);
  if (days > 0) {
    return vietnamese
      ? `hết hạn sau ${days} ngày`
      : `expires in ${days} day${days === 1 ? "" : "s"}`;
  }
  if (days < 0) {
    const overdue = -days;
    return vietnamese
      ? `đã hết hạn ${overdue} ngày trước`
      : `expired ${overdue} day${overdue === 1 ? "" : "s"} ago`;
  }
  if (then >= now) {
    return vietnamese ? "hết hạn hôm nay" : "expires today";
  }
  return vietnamese ? "đã hết hạn hôm nay" : "expired today";
}
