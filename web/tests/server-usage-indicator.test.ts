import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import ServerUsageBar from "../src/features/servers/components/ServerUsageBar.vue";
import { usageIndicator } from "../src/features/servers/utils/serverDetailView";

describe("usageIndicator", () => {
  it("uses the success level below 70%", () => {
    for (const value of [0, 69]) {
      expect(usageIndicator(value)).toEqual({
        percentage: value,
        level: "normal",
        color: "var(--success)",
      });
    }
  });

  it("uses the warning level from 70% up to 89%", () => {
    for (const value of [70, 89]) {
      expect(usageIndicator(value)).toEqual({
        percentage: value,
        level: "warning",
        color: "var(--warn)",
      });
    }
  });

  it("uses the critical level from 90% up to 100%", () => {
    for (const value of [90, 100]) {
      expect(usageIndicator(value)).toEqual({
        percentage: value,
        level: "critical",
        color: "var(--danger)",
      });
    }
  });

  it("accepts backend fractions and clamps out-of-range values", () => {
    expect(usageIndicator(0.69)?.percentage).toBe(69);
    expect(usageIndicator(120)?.percentage).toBe(100);
    expect(usageIndicator(-0.2)?.percentage).toBe(0);
  });

  it("returns no indicator when the node has not reported a value", () => {
    expect(usageIndicator(null)).toBeNull();
    expect(usageIndicator(undefined)).toBeNull();
    expect(usageIndicator(Number.NaN)).toBeNull();
  });
});

describe("ServerUsageBar", () => {
  it("shows a labelled bar with the percentage", () => {
    const wrapper = mount(ServerUsageBar, {
      props: { label: "CPU usage", value: 70 },
    });
    expect(wrapper.get('[role="progressbar"]').attributes("aria-label")).toBe(
      "CPU usage: 70%",
    );
    expect(wrapper.text()).toContain("70%");
  });

  it("shows a neutral dash without a bar when no value was reported", () => {
    const wrapper = mount(ServerUsageBar, {
      props: { label: "CPU usage", value: null },
    });
    expect(wrapper.find('[role="progressbar"]').exists()).toBe(false);
    expect(wrapper.text()).toBe("—");
  });
});
