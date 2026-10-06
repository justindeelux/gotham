/**
 * Pure geometry for MetricsChart, the dependency-free SVG time-series chart.
 * No Vue components, no DOM: every function is unit-tested through
 * chart-model.test.ts. Only tickLabel reads the UI locale (for its Intl tag
 * via shared/i18n/locale, which itself only needs Vue's ref).
 */
import { localeTag } from "@/shared/i18n/locale";
import type { Locale } from "@/shared/i18n/locale";

/** One point of a series; `at` is the bucket timestamp in milliseconds. */
export interface ChartPoint {
  at: number;
  value: number;
}

/** One named line drawn on the chart. */
export interface ChartSeries {
  name: string;
  /** Any CSS color, including a design token such as `var(--accent)`. */
  color: string;
  points: ChartPoint[];
}

/** niceCeil rounds a value up to a readable 1/2/5×10ⁿ ceiling. */
export function niceCeil(value: number): number {
  if (!Number.isFinite(value) || value <= 0) {
    return 1;
  }
  const exponent = Math.floor(Math.log10(value));
  const magnitude = 10 ** exponent;
  const fraction = value / magnitude;
  const nice = fraction <= 1 ? 1 : fraction <= 2 ? 2 : fraction <= 5 ? 5 : 10;
  return nice * magnitude;
}

/**
 * splitSegments splits one series wherever the payload has a gap.
 * Consecutive points more than 1.5 buckets apart are a real hole in the
 * series (ListServerMetrics omits empty buckets) and start a new polyline
 * instead of being bridged — nothing is interpolated through a gap.
 */
export function splitSegments(points: ChartPoint[], stepMs: number): ChartPoint[][] {
  const sorted = [...points].sort((a, b) => a.at - b.at);
  const groups: ChartPoint[][] = [];
  let current: ChartPoint[] = [];
  const gapLimit = stepMs * 1.5;
  for (const point of sorted) {
    const previous = current[current.length - 1];
    if (previous && point.at - previous.at > gapLimit) {
      groups.push(current);
      current = [];
    }
    current.push(point);
  }
  if (current.length > 0) {
    groups.push(current);
  }
  return groups;
}

/** linePath renders one segment as an SVG polyline. */
export function linePath(
  points: ChartPoint[],
  project: (_at: number, _value: number) => { x: number; y: number },
): string {
  return points
    .map((point) => {
      const { x, y } = project(point.at, point.value);
      return `${x.toFixed(2)},${y.toFixed(2)}`;
    })
    .join(" ");
}

/** areaPath closes one segment down to the baseline for the fill. */
export function areaPath(
  points: ChartPoint[],
  project: (_at: number, _value: number) => { x: number; y: number },
  baseline: number,
): string {
  const first = points[0];
  const last = points[points.length - 1];
  if (!first || !last) {
    return "";
  }
  const closed = `${linePath(points, project)} ${project(last.at, 0).x.toFixed(2)},${baseline.toFixed(2)} ${project(first.at, 0).x.toFixed(2)},${baseline.toFixed(2)}`;
  return closed;
}

/** tickLabel renders a bucket timestamp for the axis. */
export function tickLabel(
  at: number,
  stepMs: number,
  locale?: Locale | null,
): string {
  const tag = localeTag(locale);
  const date = new Date(at);
  if (stepMs >= 24 * 60 * 60_000) {
    return date.toLocaleDateString(tag, {
      day: "numeric",
      month: "short",
    });
  }
  return date.toLocaleTimeString(tag, {
    hour: "2-digit",
    minute: "2-digit",
  });
}
