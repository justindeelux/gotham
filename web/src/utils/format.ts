/**
 * Small display formatters shared by the server UI. Deliberately dependency-free
 * so the bundle stays lean and the helpers are trivial to unit-test later.
 */

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

/** relativeTime renders an ISO timestamp as a short "x ago" string. */
export function relativeTime(iso: string | null | undefined): string {
  if (!iso) {
    return "never";
  }

  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return "unknown";
  }

  const seconds = Math.round((Date.now() - then) / 1000);
  if (seconds < 45) {
    return "just now";
  }

  const minutes = Math.round(seconds / 60);
  if (minutes < 60) {
    return `${minutes}m ago`;
  }

  const hours = Math.round(minutes / 60);
  if (hours < 24) {
    return `${hours}h ago`;
  }

  const days = Math.round(hours / 24);
  if (days < 30) {
    return `${days}d ago`;
  }

  const months = Math.round(days / 30);
  if (months < 12) {
    return `${months}mo ago`;
  }

  return `${Math.round(months / 12)}y ago`;
}

/** formatDate renders an ISO timestamp as a short absolute date ("28 Sep 2026"). */
export function formatDate(iso: string | null | undefined): string {
  if (!iso) {
    return emptyPlaceholder;
  }
  const time = new Date(iso);
  if (Number.isNaN(time.getTime())) {
    return "unknown";
  }
  return time.toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}

/** expiryLabel renders how far away (or past) an expiry timestamp is. */
export function expiryLabel(iso: string | null | undefined): string {
  if (!iso) {
    return emptyPlaceholder;
  }
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return "unknown";
  }

  const now = Date.now();
  const days = Math.round((then - now) / 86_400_000);
  if (days > 0) {
    return `expires in ${days} day${days === 1 ? "" : "s"}`;
  }
  if (days < 0) {
    const overdue = -days;
    return `expired ${overdue} day${overdue === 1 ? "" : "s"} ago`;
  }
  return then >= now ? "expires today" : "expired today";
}
