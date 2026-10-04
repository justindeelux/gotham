// Unit tests for the deployment duration formatter extracted from
// ApplicationDetailPage during the JUS-24 split (F2-applications).

import { describe, expect, it, vi } from "vitest";

import type { Deployment } from "../src/features/applications/api/applications";
import { durationText } from "../src/features/applications/utils/deploymentDuration";

function deployment(startedAt: string, finishedAt: string): Deployment {
  return {
    started_at: startedAt,
    finished_at: finishedAt,
  } as Deployment;
}

describe("durationText", () => {
  it("renders seconds below a minute", () => {
    expect(durationText(deployment("2026-01-01T00:00:00Z", "2026-01-01T00:00:45Z"))).toBe("45s");
  });

  it("renders minutes and seconds below an hour", () => {
    expect(durationText(deployment("2026-01-01T00:00:00Z", "2026-01-01T00:02:05Z"))).toBe("2m5s");
  });

  it("renders hours and minutes above an hour", () => {
    expect(durationText(deployment("2026-01-01T00:00:00Z", "2026-01-01T02:03:00Z"))).toBe("2h3m");
  });

  it("renders an em dash without a start time or on inverted ranges", () => {
    expect(durationText({ started_at: "" } as Deployment)).toBe("—");
    expect(durationText(deployment("2026-01-01T00:05:00Z", "2026-01-01T00:00:00Z"))).toBe("—");
    expect(durationText(deployment("not-a-date", "2026-01-01T00:00:00Z"))).toBe("—");
  });

  it("measures against now while the deployment is unfinished", () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(new Date("2026-01-01T00:01:30Z"));
      expect(durationText({ started_at: "2026-01-01T00:00:00Z" } as Deployment)).toBe("1m30s");
    } finally {
      vi.useRealTimers();
    }
  });
});
