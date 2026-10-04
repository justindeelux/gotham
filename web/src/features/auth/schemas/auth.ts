import type { FormRules } from "naive-ui";
import { z } from "zod";

import { meetsPasswordPolicy } from "@/features/auth/utils/passwordStrength";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

/**
 * Login/register schemas (JUS-23 V1/V2). Message strings are preserved
 * verbatim from the pre-zod Naive UI rules so the migration has zero
 * user-visible diff; the strength meter stays display-only and is not a gate.
 */
export const authMessages = {
  emailRequired: "Email is required",
  emailInvalid: "Enter a valid email address",
  passwordRequired: "Password is required",
  passwordPolicy:
    "Use at least 10 characters with 2 character classes (lowercase, uppercase, digits, symbols)",
  confirmRequired: "Please confirm your password",
  confirmMismatch: "Passwords do not match",
  termsRequired: "You must accept the terms to create an account",
} as const;

/**
 * emailPattern is Naive UI's type:"email" acceptance (async-validator),
 * kept verbatim with its 320-character cap: zod's .email() rejects
 * bracket-IP, quoted-local-part and unicode-domain addresses the old rule
 * accepts, so swapping would change what is accepted.
 */
const emailPattern =
  // eslint-disable-next-line no-useless-escape -- \[ \] are verbatim from async-validator; unescaping breaks the class
  /^(([^<>()\[\]\\.,;:\s@"]+(\.[^<>()\[\]\\.,;:\s@"]+)*)|(".+"))@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}])|(([a-zA-Z\-0-9\u00A0-\uD7FF\uF900-\uFDCF\uFDF0-\uFFEF]+\.)+[a-zA-Z\u00A0-\uD7FF\uF900-\uFDCF\uFDF0-\uFFEF]{2,}))$/;

/** emailSchema backs the email field on both login and register. */
export const emailSchema = z
  .string({
    required_error: authMessages.emailRequired,
    invalid_type_error: authMessages.emailRequired,
  })
  .min(1, authMessages.emailRequired)
  .max(320, authMessages.emailInvalid)
  .regex(emailPattern, authMessages.emailInvalid);

/** loginPasswordSchema is required-only (whitespace passes, as before). */
export const loginPasswordSchema = z
  .string({
    required_error: authMessages.passwordRequired,
    invalid_type_error: authMessages.passwordRequired,
  })
  .min(1, authMessages.passwordRequired);

/**
 * loginRules builds the LoginPage NForm rules: one schema-backed entry per
 * field with the original triggers and require marks, so the module (not
 * the page) is the single source the mount tests assert against.
 */
export function loginRules(): FormRules {
  return {
    email: [{ ...ruleFrom(emailSchema, { required: true }), trigger: ["input", "blur"] }],
    password: [
      { ...ruleFrom(loginPasswordSchema, { required: true }), trigger: ["input", "blur"] },
    ],
  };
}

/** registerPasswordSchema gates on length >= 10 with >= 2 character classes. */
export const registerPasswordSchema = z
  .string({
    required_error: authMessages.passwordRequired,
    invalid_type_error: authMessages.passwordRequired,
  })
  .min(1, authMessages.passwordRequired)
  .refine((value) => meetsPasswordPolicy(value), {
    message: authMessages.passwordPolicy,
  });

/**
 * confirmPasswordSchema checks equality against the live password. It takes
 * a reader (not a snapshot) so the Naive rule sees the current password at
 * validation time, exactly like the old closure over the reactive form.
 */
export function confirmPasswordSchema(
  password: () => string,
): z.ZodEffects<z.ZodString, string, string> {
  return z
    .string({
      required_error: authMessages.confirmRequired,
      invalid_type_error: authMessages.confirmRequired,
    })
    .min(1, authMessages.confirmRequired)
    .refine((value) => value === password(), {
      message: authMessages.confirmMismatch,
    });
}

export const termsSchema = z
  .boolean({
    required_error: authMessages.termsRequired,
    invalid_type_error: authMessages.termsRequired,
  })
  .refine((value) => value === true, { message: authMessages.termsRequired });

/**
 * registerRules builds the RegisterPage NForm rules. The password reader
 * keeps the confirmation check live, exactly like the old closure over the
 * reactive form. Terms carry no require mark, as before.
 */
export function registerRules(password: () => string): FormRules {
  return {
    email: [{ ...ruleFrom(emailSchema, { required: true }), trigger: ["input", "blur"] }],
    password: [
      { ...ruleFrom(registerPasswordSchema, { required: true }), trigger: ["input", "blur"] },
    ],
    confirmPassword: [
      {
        ...ruleFrom(confirmPasswordSchema(password), { required: true }),
        trigger: ["input", "blur"],
      },
    ],
    terms: [{ ...ruleFrom(termsSchema), trigger: ["change"] }],
  };
}
