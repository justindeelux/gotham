import type { FormRules } from "naive-ui";
import { z } from "zod";

import { twoFactorCodeSchema } from "@/features/auth";
import { currentPasswordSchema } from "@/features/profile/schemas/profile";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

export const totpStatusSchema = z.object({
  enabled: z.boolean(),
  recovery_codes_remaining: z.number(),
});

export const totpEnrollmentSchema = z.object({
  secret: z.string(),
  otpauth_url: z.string(),
});

export const recoveryCodesEnvelopeSchema = z.object({
  recovery_codes: z.array(z.string()),
});

export function totpCodeRules(hasPassword: boolean): FormRules {
  return {
    ...(hasPassword
      ? {
          password: [
            {
              ...ruleFrom(currentPasswordSchema, { required: true }),
              trigger: ["input", "blur"],
            },
          ],
        }
      : {}),
    code: [
      { ...ruleFrom(twoFactorCodeSchema, { required: true }), trigger: ["input", "blur"] },
    ],
  };
}
