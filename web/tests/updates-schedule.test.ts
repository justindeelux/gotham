import { describe, expect, it } from "vitest";

import { atTimeSchema, intervalHoursSchema, scheduleStateSchema } from "@/features/updates/schemas/updates";

describe("updates schemas", () => {
  it("bounds the interval to 1-720 whole hours", () => {
    expect(intervalHoursSchema.safeParse(6).success).toBe(true);
    for (const bad of [0, 721, 1.5, null, Number.NaN]) {
      expect(intervalHoursSchema.safeParse(bad).success).toBe(false);
    }
  });

  it("accepts only 24-hour HH:MM", () => {
    expect(atTimeSchema.safeParse("03:30").success).toBe(true);
    expect(atTimeSchema.safeParse("24:00").success).toBe(false);
    expect(atTimeSchema.safeParse("3:30").success).toBe(false);
  });

  it("parses the schedule envelope", () => {
    const parsed = scheduleStateSchema.safeParse({
      schedule: {
        check_enabled: true,
        auto_apply: false,
        channel: "stable",
        frequency: "daily",
        interval_minutes: 360,
        at_time: "03:00",
        weekday: 0,
      },
      timezone: "UTC",
    });
    expect(parsed.success).toBe(true);
  });
});
