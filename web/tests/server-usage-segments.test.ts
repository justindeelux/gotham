import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import ServerUsageSegments from "../src/features/servers/components/ServerUsageSegments.vue";
import {
  isUsageStale,
  usageBlockLevel,
  usageSegments,
  usageStaleAfterMs,
} from "../src/features/servers/utils/serverDetailView";

describe("usageSegments", () => {
  it("fills round(percentage / 10) blocks", () => {
    const cases: Array<[number, number]> = [
      [0, 0],
      [4, 0],
      [5, 1],
      [60, 6],
      [61, 6],
      [70, 7],
      [80, 8],
      [85, 9],
      [95, 10],
      [100, 10],
    ];
    for (const [value, filled] of cases) {
      expect({ value, filled: usageSegments(value)?.filled }, `${value}%`).toEqual({
        value,
        filled,
      });
    }
  });

  it("keeps the gauge colours fixed by block position", () => {
    const blocks = usageSegments(100)?.blocks ?? [];
    expect(blocks.map((block) => block.level)).toEqual([
      "success",
      "success",
      "success",
      "success",
      "success",
      "success",
      "warning",
      "warning",
      "danger",
      "danger",
    ]);
    expect(usageBlockLevel(0)).toBe("success");
    expect(usageBlockLevel(5)).toBe("success");
    expect(usageBlockLevel(6)).toBe("warning");
    expect(usageBlockLevel(7)).toBe("warning");
    expect(usageBlockLevel(8)).toBe("danger");
    expect(usageBlockLevel(9)).toBe("danger");
  });

  it("marks only the filled blocks at 45%", () => {
    const view = usageSegments(45);
    expect(view?.filled).toBe(5);
    expect(view?.blocks.filter((block) => block.filled)).toHaveLength(5);
    expect(
      view?.blocks.filter((block) => block.filled).map((block) => block.level),
    ).toEqual(["success", "success", "success", "success", "success"]);
  });

  it("accepts backend fractions and clamps out-of-range values", () => {
    expect(usageSegments(0.69)?.percentage).toBe(69);
    expect(usageSegments(120)?.percentage).toBe(100);
    expect(usageSegments(120)?.filled).toBe(10);
    expect(usageSegments(-0.2)?.percentage).toBe(0);
    expect(usageSegments(-0.2)?.filled).toBe(0);
  });

  it("returns nothing when the node has not reported a value", () => {
    expect(usageSegments(null)).toBeNull();
    expect(usageSegments(undefined)).toBeNull();
    expect(usageSegments(Number.NaN)).toBeNull();
  });
});

describe("isUsageStale", () => {
  const now = Date.parse("2026-10-30T12:00:00Z");

  it("treats a report older than three minutes as stale", () => {
    expect(isUsageStale("2026-10-30T11:56:59Z", now)).toBe(true);
    expect(isUsageStale("2026-10-30T11:57:00Z", now)).toBe(false);
    expect(isUsageStale("2026-10-30T11:59:59Z", now)).toBe(false);
  });

  it("does not call a missing timestamp stale", () => {
    expect(isUsageStale(null, now)).toBe(false);
    expect(isUsageStale(undefined, now)).toBe(false);
    expect(isUsageStale("not-a-date", now)).toBe(false);
  });

  it("uses a three minute window", () => {
    expect(usageStaleAfterMs).toBe(180_000);
  });
});

describe("ServerUsageSegments", () => {
  it("renders ten positioned blocks and a labelled meter", () => {
    const wrapper = mount(ServerUsageSegments, {
      props: { label: "CPU usage", value: 62 },
    });
    const meter = wrapper.get('[role="meter"]');
    expect(meter.attributes("aria-label")).toBe("CPU usage 62%");
    expect(meter.attributes("aria-valuenow")).toBe("62");
    expect(meter.attributes("aria-valuemin")).toBe("0");
    expect(meter.attributes("aria-valuemax")).toBe("100");
    expect(wrapper.text()).toContain("62%");

    const blocks = wrapper.findAll(".usage-block");
    expect(blocks).toHaveLength(10);
    expect(wrapper.findAll(".usage-block.is-filled")).toHaveLength(6);
    expect(blocks[0]?.classes()).toContain("is-success");
    expect(blocks[6]?.classes()).toContain("is-warning");
    expect(blocks[9]?.classes()).toContain("is-danger");
  });

  it("shows a dash and no filled blocks when no value was reported", () => {
    const wrapper = mount(ServerUsageSegments, {
      props: { label: "CPU usage", value: null },
    });
    expect(wrapper.find('[role="meter"]').exists()).toBe(false);
    expect(wrapper.text()).toContain("—");
    expect(wrapper.text()).not.toContain("0%");
    expect(wrapper.findAll(".usage-block")).toHaveLength(10);
    expect(wrapper.findAll(".usage-block.is-filled")).toHaveLength(0);
  });

  it("dims a stale value without hiding the percentage", () => {
    const wrapper = mount(ServerUsageSegments, {
      props: { label: "CPU usage", value: 62, stale: true },
    });
    expect(wrapper.classes()).toContain("is-stale");
    expect(wrapper.text()).toContain("62%");
    expect(wrapper.get('[role="meter"]').attributes("aria-valuenow")).toBe("62");
  });
});
