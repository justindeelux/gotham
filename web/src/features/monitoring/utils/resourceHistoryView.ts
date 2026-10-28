import type {
  ResourceHistory,
  ResourceHistoryRange,
  ResourceHistorySeries,
} from "@/features/monitoring/api/resourceHistory";
import type { ChartSeries } from "@/shared/ui/MetricsChart.vue";
import { formatBytes } from "@/shared/utils/format";

export const resourceRanges: Array<{ value: ResourceHistoryRange; label: string }> = [
  { value: "1h", label: "1h" },
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
];

export type ResourceChartKey = "cpu" | "memory" | "network" | "disk";

export interface ResourceChart {
  key: ResourceChartKey;
  series: ChartSeries[];
  latest: string;
  latestAt: string;
  yMax?: number;
}

const palette = ["var(--accent)", "var(--success)", "var(--warn)", "var(--link)"];

export function formatCores(value: number): string {
  if (!Number.isFinite(value)) {
    return "—";
  }
  return value >= 10 ? value.toFixed(1) : value.toFixed(2);
}

export function formatRate(value: number): string {
  return `${formatBytes(value)}/s`;
}

export function serviceOptions(history: ResourceHistory | null): Array<{ label: string; value: string }> {
  const services = new Set<string>();
  for (const series of history?.series ?? []) {
    if (series.service) {
      services.add(series.service);
    }
  }
  return Array.from(services)
    .sort()
    .map((service) => ({ label: service, value: service }));
}

function seriesLabel(series: ResourceHistorySeries): string {
  if (!series.service) {
    return series.deployment_id ? shortId(series.deployment_id) : "—";
  }
  return series.deployment_id
    ? `${series.service} · ${shortId(series.deployment_id)}`
    : series.service;
}

export function shortId(value: string): string {
  return value.length > 8 ? value.slice(0, 8) : value;
}

function pointSeries(
  history: ResourceHistory,
  select: (_point: ResourceHistorySeries["points"][number]) => number | null,
): ChartSeries[] {
  const out: ChartSeries[] = [];
  history.series.forEach((series, index) => {
    const points = series.points
      .map((point) => ({
        at: new Date(point.bucket).getTime(),
        value: select(point),
      }))
      .filter((point): point is { at: number; value: number } => point.value !== null);
    if (points.length === 0) {
      return;
    }
    out.push({
      name: seriesLabel(series),
      color: palette[index % palette.length],
      points,
    });
  });
  return out;
}

function lastPoint(history: ResourceHistory): ResourceHistorySeries["points"][number] | null {
  let last: ResourceHistorySeries["points"][number] | null = null;
  for (const series of history.series) {
    for (const point of series.points) {
      if (!last || new Date(point.latest_at).getTime() > new Date(last.latest_at).getTime()) {
        last = point;
      }
    }
  }
  return last;
}

export function buildResourceCharts(history: ResourceHistory | null): ResourceChart[] {
  if (!history) {
    return [];
  }
  const last = lastPoint(history);
  const latestAt = last?.latest_at ?? "";
  return [
    {
      key: "cpu",
      series: pointSeries(history, (point) => point.cpu_cores),
      latest: last?.cpu_cores != null ? formatCores(last.cpu_cores) : "—",
      latestAt,
    },
    {
      key: "memory",
      series: pointSeries(history, (point) => point.mem_working_set_bytes),
      latest: last?.mem_working_set_bytes != null ? formatBytes(last.mem_working_set_bytes) : "—",
      latestAt,
    },
    {
      key: "network",
      series: [
        ...pointSeries(history, (point) => point.net_rx_bps).map((series) => ({
          ...series,
          name: `${series.name} ↓`,
        })),
        ...pointSeries(history, (point) => point.net_tx_bps).map((series) => ({
          ...series,
          name: `${series.name} ↑`,
        })),
      ],
      latest: rateSummary(last, "net_rx_bps", "net_tx_bps", "↓", "↑"),
      latestAt,
    },
    {
      key: "disk",
      series: [
        ...pointSeries(history, (point) => point.disk_read_bps).map((series) => ({
          ...series,
          name: `${series.name} read`,
        })),
        ...pointSeries(history, (point) => point.disk_write_bps).map((series) => ({
          ...series,
          name: `${series.name} write`,
        })),
      ],
      latest: rateSummary(last, "disk_read_bps", "disk_write_bps", "read", "write"),
      latestAt,
    },
  ];
}

function rateSummary(
  point: ResourceHistorySeries["points"][number] | null,
  first: "net_rx_bps" | "disk_read_bps",
  second: "net_tx_bps" | "disk_write_bps",
  firstLabel: string,
  secondLabel: string,
): string {
  if (!point) {
    return "—";
  }
  const parts: string[] = [];
  if (point[first] != null) {
    parts.push(`${formatRate(point[first])} ${firstLabel}`);
  }
  if (point[second] != null) {
    parts.push(`${formatRate(point[second])} ${secondLabel}`);
  }
  return parts.length > 0 ? parts.join(" · ") : "—";
}

export function memoryLimitLabel(history: ResourceHistory | null): string {
  const last = history ? lastPoint(history) : null;
  if (!last) {
    return "";
  }
  if (last.mem_unlimited) {
    return "unlimited";
  }
  if (last.mem_limit_bytes > 0) {
    return formatBytes(last.mem_limit_bytes);
  }
  return "unknown";
}

export function chartStepMs(history: ResourceHistory | null): number {
  switch (history?.step) {
    case "30s":
      return 30_000;
    case "5m":
      return 5 * 60_000;
    case "1h":
      return 60 * 60_000;
    default:
      return 60_000;
  }
}

export function markerCount(history: ResourceHistory | null): number {
  return history?.markers.length ?? 0;
}
