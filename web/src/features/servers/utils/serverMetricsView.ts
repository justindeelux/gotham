// Pure view helpers for the server metrics tab (extracted from ServerDetailPage).

import type { MetricPoint, MetricStep } from "@/features/servers/api/metrics";
import { isMetricsDisabled } from "@/features/servers/api/metrics";
import { failureText, isApiError, stripErrorPrefix } from "@/features/servers/api/servers";
import { activeLocale } from "@/shared/i18n/locale";
import type { ChartSeries } from "@/shared/ui/MetricsChart.vue";
import { formatBytes, toPercent } from "@/shared/utils/format";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

/** catalogFor selects the servers display dictionary for one locale. */
function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

/** One metrics range: the API step and the window it is shown over. */
export interface MetricRange {
  step: MetricStep;
  /** Bucket width of the step, in milliseconds. */
  stepMs: number;
  /** Window read at this step; capped by ParseMetricsQuery per step. */
  windowMs: number;
  /** Shown above the charts. */
  hint: string;
}

export const metricRanges: MetricRange[] = [
  { step: "1m", stepMs: 60_000, windowMs: 60 * 60_000, hint: "Step 1m · last hour" },
  {
    step: "1h",
    stepMs: 60 * 60_000,
    windowMs: 24 * 60 * 60_000,
    hint: "Step 1h · last 24 hours",
  },
  {
    step: "1d",
    stepMs: 24 * 60 * 60_000,
    windowMs: 7 * 24 * 60 * 60_000,
    hint: "Step 1d · last 7 days",
  },
];

/** metricRangeHint renders the window hint for one step in the display locale. */
export function metricRangeHint(step: MetricStep, locale?: string | null): string {
  const metrics = catalogFor(locale).metrics;
  switch (step) {
    case "1h":
      return metrics.range1h;
    case "1d":
      return metrics.range1d;
    case "1m":
    default:
      return metrics.range1m;
  }
}
/** Auto-refresh choices: off unless the operator picks a cadence. */
export type MetricRefreshChoice = "off" | "15s" | "60s";

export const metricRefreshOptions: Array<{ label: string; value: MetricRefreshChoice }> = [
  { label: "off", value: "off" },
  { label: "15s", value: "15s" },
  { label: "60s", value: "60s" },
];

/** refreshMsForChoice is the auto-refresh cadence in ms; 0 keeps it off. */
export function refreshMsForChoice(choice: MetricRefreshChoice): number {
  switch (choice) {
    case "15s":
      return 15_000;
    case "60s":
      return 60_000;
    default:
      return 0;
  }
}

/**
 * usage reads a 0..1 usage fraction as a percentage. The heartbeat contract
 * stores usage as a fraction (see toPercent), which is what the rollup keeps.
 */
export function usage(value: number): number {
  return toPercent(value);
}

/** formatPercentValue renders a percentage axis label. */
export function formatPercentValue(value: number): string {
  return `${Math.round(value)}%`;
}

/** formatRateValue renders a byte-per-second axis label. */
export function formatRateValue(value: number): string {
  return `${formatBytes(value)}/s`;
}

/** One rendered card: its series, axis and newest reading. */
export interface MetricChart {
  title: string;
  /** Percentage charts share a fixed 0..100 axis. */
  percent: boolean;
  series: ChartSeries[];
  /** The newest sample, rendered next to the title. */
  latest: string;
}

/**
 * buildMetricCharts maps the window's points onto the four real charts.
 * Nothing is synthesized: every series is a projection of the given `points`.
 */
export function buildMetricCharts(
  points: MetricPoint[],
  locale?: string | null,
): MetricChart[] {
  const metrics = catalogFor(locale).metrics;
  const pointsOf = (select: (_point: MetricPoint) => number): ChartSeries["points"] =>
    points.map((point) => ({
      at: new Date(point.bucket).getTime(),
      value: select(point),
    }));
  const last = points[points.length - 1];
  const rx = last ? formatRateValue(last.net_rx_bps) : "";
  const tx = last ? formatRateValue(last.net_tx_bps) : "";
  const read = last ? formatRateValue(last.disk_read_bps) : "";
  const write = last ? formatRateValue(last.disk_write_bps) : "";
  return [
    {
      title: metrics.chartCpu,
      percent: true,
      series: [
        { name: metrics.seriesCpu, color: "var(--accent)", points: pointsOf((p) => usage(p.cpu_usage)) },
      ],
      latest: last ? formatPercentValue(usage(last.cpu_usage)) : "",
    },
    {
      title: metrics.chartRam,
      percent: true,
      series: [
        { name: metrics.seriesRam, color: "var(--success)", points: pointsOf((p) => usage(p.mem_usage)) },
      ],
      latest: last ? formatPercentValue(usage(last.mem_usage)) : "",
    },
    {
      title: metrics.chartDisk,
      percent: false,
      series: [
        { name: metrics.seriesRead, color: "var(--accent)", points: pointsOf((p) => p.disk_read_bps) },
        { name: metrics.seriesWrite, color: "var(--warn)", points: pointsOf((p) => p.disk_write_bps) },
      ],
      latest: last ? `${read} ↓ · ${write} ↑` : "",
    },
    {
      title: metrics.chartNetwork,
      percent: false,
      series: [
        { name: metrics.seriesRx, color: "var(--accent)", points: pointsOf((p) => p.net_rx_bps) },
        { name: metrics.seriesTx, color: "var(--warn)", points: pointsOf((p) => p.net_tx_bps) },
      ],
      latest: last ? `${rx} ↓ · ${tx} ↑` : "",
    },
  ];
}

/** isMetricStep narrows a server-returned step onto the accepted values. */
export function isMetricStep(value: string): value is MetricStep {
  return value === "1m" || value === "1h" || value === "1d";
}

/**
 * metricsFailureText renders a retained metrics failure for display in the
 * current locale. Curated refusals (expired session, disabled feature flag)
 * keep their summary-only shape; anything else gets the localized summary
 * plus the raw diagnostic. Classification stays on the raw error object.
 */
export function metricsFailureText(
  failure: unknown,
  locale?: string | null,
): string | null {
  if (failure === null || failure === undefined) {
    return null;
  }
  const errors = catalogFor(locale).errors;
  if (isApiError(failure) && failure.status === 401) {
    return errors.sessionExpired;
  }
  if (isMetricsDisabled(failure)) {
    return errors.metricsDisabled;
  }
  if (isApiError(failure) && failure.status === 400) {
    const detail = stripErrorPrefix(failure.message ?? "");
    return detail === "" ? errors.metricsRange : failureText(failure, locale);
  }
  return failureText(failure, locale);
}
