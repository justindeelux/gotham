// Pure geometry for MetricsChart, the dependency-free SVG time-series chart.
import { describe, expect, it } from "vitest";

import {
  areaPath,
  linePath,
  niceCeil,
  splitSegments,
  tickLabel,
} from "../src/shared/ui/chartModel";

const project = (at: number, value: number): { x: number; y: number } => ({
  x: at,
  y: value,
});

describe("niceCeil", () => {
  it("rounds up to a readable 1/2/5×10ⁿ ceiling", () => {
    expect(niceCeil(0)).toBe(1);
    expect(niceCeil(-3)).toBe(1);
    expect(niceCeil(0.3)).toBe(0.5);
    expect(niceCeil(3)).toBe(5);
    expect(niceCeil(7)).toBe(10);
    expect(niceCeil(100)).toBe(100);
  });
});

describe("splitSegments", () => {
  it("bridges close buckets and breaks real gaps", () => {
    const points = [
      { at: 0, value: 1 },
      { at: 60_000, value: 2 },
      { at: 300_000, value: 3 },
    ];
    expect(splitSegments(points.slice(0, 2), 60_000)).toHaveLength(1);
    expect(splitSegments(points, 60_000)).toHaveLength(2);
  });

  it("sorts unordered payloads", () => {
    const points = [
      { at: 60_000, value: 2 },
      { at: 0, value: 1 },
    ];
    const [group] = splitSegments(points, 60_000);
    expect(group.map((point) => point.at)).toEqual([0, 60_000]);
  });
});

describe("linePath/areaPath", () => {
  it("renders segments and closes the fill", () => {
    const points = [
      { at: 0, value: 10 },
      { at: 60_000, value: 20 },
    ];
    expect(linePath(points, project)).toBe("0.00,10.00 60000.00,20.00");
    expect(areaPath(points, project, 100)).toBe(
      "0.00,10.00 60000.00,20.00 60000.00,100.00 0.00,100.00",
    );
    expect(areaPath([], project, 100)).toBe("");
  });
});

describe("tickLabel", () => {
  it("renders times for sub-day buckets and dates for day buckets", () => {
    expect(tickLabel(Date.UTC(2026, 0, 5, 14, 5), 60_000)).toMatch(/\d{2}:\d{2}/);
    expect(tickLabel(Date.UTC(2026, 0, 5), 24 * 60 * 60_000)).not.toBe("");
  });
});
