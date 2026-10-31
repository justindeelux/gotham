import { describe, expect, it } from "vitest";

import {
  cleanupRunListSchema,
  scheduleFormSchema,
} from "@/features/cleanup/schemas/center";
import { cronPresetKey, cronPresets } from "@/features/cleanup/utils/cron";
import { parseWith } from "@/shared/validation/parse";

function form(overrides: Record<string, unknown> = {}) {
  return {
    name: "nightly",
    server_ids: ["node-1"],
    categories: ["build_cache"],
    cron: "0 3 * * *",
    min_age_hours: 24,
    enabled: true,
    ...overrides,
  };
}

describe("cleanup schedule form", () => {
  it("accepts a complete schedule", () => {
    expect(scheduleFormSchema.safeParse(form()).success).toBe(true);
  });

  it("never accepts a dangerous category", () => {
    const result = scheduleFormSchema.safeParse(
      form({ categories: ["build_cache", "orphaned_volumes"] }),
    );
    expect(result.success).toBe(false);
  });

  it("requires a name, at least one node and one category", () => {
    expect(scheduleFormSchema.safeParse(form({ name: "  " })).success).toBe(false);
    expect(scheduleFormSchema.safeParse(form({ server_ids: [] })).success).toBe(false);
    expect(scheduleFormSchema.safeParse(form({ categories: [] })).success).toBe(false);
  });

  it("bounds the minimum age to the server contract", () => {
    expect(scheduleFormSchema.safeParse(form({ min_age_hours: 0 })).success).toBe(false);
    expect(scheduleFormSchema.safeParse(form({ min_age_hours: 8761 })).success).toBe(false);
    expect(scheduleFormSchema.safeParse(form({ min_age_hours: 1.5 })).success).toBe(false);
  });
});

describe("cron presets", () => {
  it("maps the presets to their expressions", () => {
    expect(cronPresets.map((preset) => preset.cron)).toEqual([
      "0 3 * * *",
      "0 3 * * 0",
    ]);
  });

  it("reports a preset for a matching expression and custom otherwise", () => {
    expect(cronPresetKey("0 3 * * *")).toBe("daily");
    expect(cronPresetKey(" 0 3 * * 0 ")).toBe("weekly");
    expect(cronPresetKey("*/15 * * * *")).toBe("custom");
  });
});

describe("cleanup history parsing", () => {
  it("parses runs with category totals and skipped reasons", () => {
    const parsed = parseWith(cleanupRunListSchema, {
      runs: [
        {
          id: "run-1",
          server_id: "node-1",
          trigger: "schedule",
          schedule_name: "nightly",
          started_at: "2026-10-31T03:00:00Z",
          finished_at: "2026-10-31T03:01:00Z",
          reclaimed_bytes: 2048,
          status: "skipped",
          reason: "busy",
          categories: { build_cache: 2048 },
        },
      ],
    });
    expect(parsed.runs?.[0]?.schedule_name).toBe("nightly");
    expect(parsed.runs?.[0]?.categories?.build_cache).toBe(2048);
  });
});
