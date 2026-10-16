import type { FormRules } from "naive-ui";
import { z } from "zod";

import { twoFactorCodeSchema } from "@/features/auth";
import { currentPasswordSchema } from "@/features/profile/schemas/profile";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

/**
 * Two-factor (TOTP) transport validation (JUS-196). The code field schema is
 * shared with the login step (features/auth), so the same input is accepted
 * wherever a code is asked for.
 */

/** totpStatusSchema validates GET /auth/me/totp. */
export const totpStatusSchema = z.object({
  enabled: z.boolean(),
  recovery_codes_remaining: z.number(),
});

/** totpEnrollmentSchema validates POST /auth/me/totp/enroll. */
export const totpEnrollmentSchema = z.object({
  secret: z.string(),
  otpauth_url: z.string(),
});

/** recoveryCodesEnvelopeSchema validates the enable/regenerate replies. */
export const recoveryCodesEnvelopeSchema = z.object({
  recovery_codes: z.array(z.string()),
});

/**
 * totpCodeRules builds the NForm rules for the code (and, when the account has
 * a password, the current-password confirmation) shared by the enroll, disable
 * and regenerate dialogs.
 */
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
