import { describe, expect, it } from "vitest";

import {
  cleanupBackupFileMinAgeDaysSchema,
  cleanupHistoryDaysSchema,
  cleanupRetentionSchema,
  stateEnvelopeSchema,
} from "@/features/instance-settings/schemas/instance";

describe("cleanup retention form schema", () => {
  it("accepts the documented bounds", () => {
    expect(cleanupHistoryDaysSchema.safeParse(7).success).toBe(true);
    expect(cleanupHistoryDaysSchema.safeParse(3650).success).toBe(true);
    expect(cleanupBackupFileMinAgeDaysSchema.safeParse(1).success).toBe(true);
    expect(cleanupBackupFileMinAgeDaysSchema.safeParse(365).success).toBe(true);
  });

  it("rejects values outside the bounds and fractions", () => {
    expect(cleanupHistoryDaysSchema.safeParse(6).success).toBe(false);
    expect(cleanupHistoryDaysSchema.safeParse(3651).success).toBe(false);
    expect(cleanupHistoryDaysSchema.safeParse(90.5).success).toBe(false);
    expect(cleanupBackupFileMinAgeDaysSchema.safeParse(0).success).toBe(false);
    expect(cleanupBackupFileMinAgeDaysSchema.safeParse(366).success).toBe(false);
    expect(cleanupBackupFileMinAgeDaysSchema.safeParse(null).success).toBe(false);
    expect(cleanupHistoryDaysSchema.safeParse(6).error?.issues[0]?.message).toBe(
      "instance-settings.validation.cleanupHistory",
    );
  });

  it("requires both windows together", () => {
    expect(
      cleanupRetentionSchema.safeParse({
        history_days: 90,
        backup_file_min_age_days: 7,
      }).success,
    ).toBe(true);
    expect(cleanupRetentionSchema.safeParse({ history_days: 90 }).success).toBe(false);
  });
});

describe("instance settings envelope", () => {
  it("carries the cleanup retention windows next to general settings", () => {
    const parsed = stateEnvelopeSchema.safeParse({
      settings: {
        general: {
          control_plane_url: { value: "", source: "default", locked: false },
          instance_name: { value: "gotham", source: "default", locked: false },
        },
        cleanup: { history_days: 90, backup_file_min_age_days: 7 },
      },
    });
    expect(parsed.success).toBe(true);
  });
});
