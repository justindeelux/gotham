import { describe, expect, it } from "vitest";

import type { ResourceHistory } from "../src/features/monitoring/api/resourceHistory";
import {
  buildResourceCharts,
  chartStepMs,
  formatCoresForMax,
  formatRate,
  maxCPUCores,
  memoryLimitLabel,
  serviceOptions,
} from "../src/features/monitoring/utils/resourceHistoryView";

function history(): ResourceHistory {
  const base = new Date("2026-10-01T00:00:00Z");
  const at = (offset: number): string =>
    new Date(base.getTime() + offset).toISOString();
  return {
    range: "1h",
    step: "30s",
    from: at(-3_600_000),
    to: at(0),
    capability: "available",
    coverage: "partial",
    markers: [
      { at: at(-1_800_000), deployment_id: "deploy-abcdef123", service: "web" },
    ],
    series: [
      {
        service: "web",
        deployment_id: "deploy-abcdef123",
        points: [
          {
            bucket: at(-60_000),
            cpu_cores: 0.5,
            mem_working_set_bytes: 2048,
            mem_limit_bytes: 0,
            mem_unlimited: true,
            net_rx_bps: 100,
            net_tx_bps: null,
            disk_read_bps: null,
            disk_write_bps: null,
            latest_at: at(-50_000),
            replicas: 1,
            expected_replicas: 2,
            coverage: "partial",
          },
        ],
      },
    ],
  };
}

describe("resource history view", () => {
  it("keeps tiny CPU axis ticks pairwise distinct", () => {
    const millicores = formatCoresForMax(0.008);
    expect([0, 0.005, 0.01].map(millicores)).toEqual(["0 m", "5 m", "10 m"]);
    expect(new Set([0, 0.005, 0.01].map(millicores)).size).toBe(3);

    const subMilli = formatCoresForMax(0.0008);
    expect([0, 0.0005, 0.001].map(subMilli)).toEqual([
      "0.0 m",
      "0.5 m",
      "1.0 m",
    ]);
    expect(new Set([0, 0.0005, 0.001].map(subMilli)).size).toBe(3);

    const cores = formatCoresForMax(0.5);
    expect([0, 0.25, 0.5].map(cores)).toEqual(["0.00", "0.25", "0.50"]);
  });

  it("uses the CPU maximum and the same formatter for the latest value", () => {
    const sample = history();
    sample.series[0]!.points[0]!.cpu_cores = 0.006;
    expect(maxCPUCores(sample)).toBe(0.006);
    const chart = buildResourceCharts(sample).find((item) => item.key === "cpu");
    expect(chart?.latest).toBe("6 m");
  });

  it("formats tiny rate axis ticks without undefined units", () => {
    expect(formatRate(0)).toBe("0 B/s");
    expect(formatRate(0.5)).toBe("0.50 B/s");
    expect(formatRate(1)).toBe("1 B/s");
    expect(formatRate(1536)).toBe("1.50 KiB/s");
  });

  it("keeps an unknown metric as a gap instead of a zero", () => {
    const charts = buildResourceCharts(history());
    const network = charts.find((chart) => chart.key === "network");
    expect(network?.series).toHaveLength(1);
    expect(network?.series[0]?.name).toContain("↓");
    expect(network?.latest).toBe("100 B/s ↓");
    const disk = charts.find((chart) => chart.key === "disk");
    expect(disk?.series).toHaveLength(0);
    expect(disk?.latest).toBe("—");
  });

  it("labels an unlimited memory limit without inventing a percentage", () => {
    expect(memoryLimitLabel(history())).toBe("unlimited");
  });

  it("offers the services present in the response and the range step", () => {
    expect(serviceOptions(history())).toEqual([{ label: "web", value: "web" }]);
    expect(chartStepMs(history())).toBe(30_000);
    expect(chartStepMs({ ...history(), step: "5m" })).toBe(300_000);
  });
});
