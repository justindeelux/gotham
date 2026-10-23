import { describe, expect, it } from "vitest";

import { twoFactorCodeSchema } from "@/features/auth";
import {
  recoveryCodesEnvelopeSchema,
  totpEnrollmentSchema,
  totpStatusSchema,
} from "@/features/profile";

describe("twoFactorCodeSchema", () => {
  it("accepts a 6-digit authenticator code", () => {
    expect(twoFactorCodeSchema.safeParse("123456").success).toBe(true);
  });

  it("accepts a formatted recovery code", () => {
    expect(twoFactorCodeSchema.safeParse("ABCDE-FGHJK").success).toBe(true);
  });

  it("rejects too-short or unusable input", () => {
    expect(twoFactorCodeSchema.safeParse("123").success).toBe(false);
    expect(twoFactorCodeSchema.safeParse("12345!").success).toBe(false);
    expect(twoFactorCodeSchema.safeParse("").success).toBe(false);
  });
});

describe("transport envelopes", () => {
  it("validates the status shape", () => {
    expect(
      totpStatusSchema.safeParse({ enabled: false, recovery_codes_remaining: 0 }).success,
    ).toBe(true);
    expect(totpStatusSchema.safeParse({ enabled: "no" }).success).toBe(false);
  });

  it("validates the enrollment shape", () => {
    expect(
      totpEnrollmentSchema.safeParse({ secret: "S", otpauth_url: "otpauth://totp/x" }).success,
    ).toBe(true);
  });

  it("validates the recovery-code envelope", () => {
    expect(recoveryCodesEnvelopeSchema.safeParse({ recovery_codes: ["A", "B"] }).success).toBe(
      true,
    );
    expect(recoveryCodesEnvelopeSchema.safeParse({ recovery_codes: "A" }).success).toBe(false);
  });
});
