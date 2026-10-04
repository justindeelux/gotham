// Unit tests for the server metrics view helpers (JUS-24 split of ServerDetailPage).

import { describe, expect, it } from "vitest";

import type { MetricPoint } from "../src/features/servers/api/metrics";
import {
  buildMetricCharts,
  formatPercentValue,
  formatRateValue,
  isMetricStep,
  refreshMsForChoice,
  usage,
} from "../src/features/servers/utils/serverMetricsView";

function point(overrides: Partial<MetricPoint> = {}): MetricPoint {
  return {
    bucket: "2026-03-01T12:00:00Z",
    cpu_usage: 0.5,
    mem_usage: 0.25,
    disk_usage: 0.75,
    net_rx_bps: 1024,
    net_tx_bps: 2048,
    disk_read_bps: 4096,
    disk_write_bps: 8192,
    container_count: 2,
    ...overrides,
  };
}

describe("usage", () => {
  it("reads a 0..1 fraction as a percentage", () => {
    expect(usage(0.5)).toBe(50);
  });
});

describe("axis labels", () => {
  it("renders percent and byte-rate labels", () => {
    expect(formatPercentValue(12.6)).toBe("13%");
    expect(formatRateValue(2048)).toBe("2.00 KiB/s");
  });
});

describe("refreshMsForChoice", () => {
  it("maps cadences onto milliseconds with off as zero", () => {
    expect(refreshMsForChoice("off")).toBe(0);
    expect(refreshMsForChoice("15s")).toBe(15_000);
    expect(refreshMsForChoice("60s")).toBe(60_000);
  });
});

describe("isMetricStep", () => {
  it("narrows onto the accepted steps", () => {
    expect(isMetricStep("1m")).toBe(true);
    expect(isMetricStep("1h")).toBe(true);
    expect(isMetricStep("1d")).toBe(true);
    expect(isMetricStep("5m")).toBe(false);
  });
});

describe("buildMetricCharts", () => {
  it("projects points onto the four real charts with the newest reading", () => {
    const charts = buildMetricCharts([point()]);
    expect(charts.map((chart) => chart.title)).toEqual(["CPU", "RAM", "Disk I/O", "Network"]);
    expect(charts[0].latest).toBe("50%");
    expect(charts[0].percent).toBe(true);
    expect(charts[2].series).toHaveLength(2);
    expect(charts[0].series[0].points).toHaveLength(1);
  });

  it("renders empty readings without samples", () => {
    const charts = buildMetricCharts([]);
    expect(charts).toHaveLength(4);
    expect(charts.every((chart) => chart.latest === "")).toBe(true);
  });
});
