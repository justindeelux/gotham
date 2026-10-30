import { describe, expect, it } from "vitest";

import {
  controlPlaneUrlSchema,
  instanceNameSchema,
} from "@/features/instance-settings/schemas/instance";

describe("instance-settings schemas", () => {
  it("validates the control-plane URL", () => {
    expect(controlPlaneUrlSchema.safeParse("https://g.example.com").success).toBe(true);
    expect(controlPlaneUrlSchema.safeParse("").success).toBe(true);
    for (const bad of ["ftp://x.io", "g.example.com", "https://u:p@x.io", "https://x.io/?a=1"]) {
      expect(controlPlaneUrlSchema.safeParse(bad).success, bad).toBe(false);
    }
  });

  it("validates the instance name", () => {
    expect(instanceNameSchema.safeParse("gotham").success).toBe(true);
    expect(instanceNameSchema.safeParse("  homelab  ").success).toBe(true);
    expect(instanceNameSchema.safeParse("").success).toBe(false);
    expect(instanceNameSchema.safeParse("x".repeat(65)).success).toBe(false);
  });
});
