import type { FormRules } from "naive-ui";
import { z } from "zod";

import { meetsPasswordPolicy } from "@/features/auth/utils/passwordStrength";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

export const authMessages = {
  emailRequired: "auth.validation.emailRequired",
  emailInvalid: "auth.validation.emailInvalid",
  passwordRequired: "auth.validation.passwordRequired",
  passwordPolicy: "auth.validation.passwordPolicy",
  confirmRequired: "auth.validation.confirmRequired",
  confirmMismatch: "auth.validation.confirmMismatch",
  termsRequired: "auth.validation.termsRequired",
} as const;

const emailPattern =
  // eslint-disable-next-line no-useless-escape -- \[ \] are verbatim from async-validator; unescaping breaks the class
  /^(([^<>()\[\]\\.,;:\s@"]+(\.[^<>()\[\]\\.,;:\s@"]+)*)|(".+"))@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}])|(([a-zA-Z\-0-9\u00A0-\uD7FF\uF900-\uFDCF\uFDF0-\uFFEF]+\.)+[a-zA-Z\u00A0-\uD7FF\uF900-\uFDCF\uFDF0-\uFFEF]{2,}))$/;

export const emailSchema = z
  .string({
    error: authMessages.emailRequired,
  })
  .min(1, authMessages.emailRequired)
  .max(320, authMessages.emailInvalid)
  .regex(emailPattern, authMessages.emailInvalid);

export const loginPasswordSchema = z
  .string({
    error: authMessages.passwordRequired,
  })
  .min(1, authMessages.passwordRequired);

export function loginRules(): FormRules {
  return {
    email: [{ ...ruleFrom(emailSchema, { required: true }), trigger: ["input", "blur"] }],
    password: [
      { ...ruleFrom(loginPasswordSchema, { required: true }), trigger: ["input", "blur"] },
    ],
  };
}

export const registerPasswordSchema = z
  .string({
    error: authMessages.passwordRequired,
  })
  .min(1, authMessages.passwordRequired)
  .refine((value) => meetsPasswordPolicy(value), {
    message: authMessages.passwordPolicy,
  });

export function confirmPasswordSchema(
  password: () => string,
): z.ZodType<string, string> {
  return z
    .string({
      error: authMessages.confirmRequired,
    })
    .min(1, authMessages.confirmRequired)
    .refine((value) => value === password(), {
      message: authMessages.confirmMismatch,
    });
}

export const termsSchema = z
  .boolean({
    error: authMessages.termsRequired,
  })
  .refine((value) => value === true, { message: authMessages.termsRequired });

export const twoFactorMessages = {
  codeRequired: "auth.validation.twoFactorCodeRequired",
  codeInvalid: "auth.validation.twoFactorCodeInvalid",
} as const;

export const twoFactorCodeSchema = z
  .string({
    error: twoFactorMessages.codeRequired,
  })
  .trim()
  .min(6, twoFactorMessages.codeInvalid)
  .max(14, twoFactorMessages.codeInvalid)
  .regex(/^[A-Za-z0-9 -]+$/, twoFactorMessages.codeInvalid);

export function twoFactorCodeRules(): FormRules {
  return {
    code: [
      {
        ...ruleFrom(twoFactorCodeSchema, { required: true }),
        trigger: ["input", "blur"],
      },
    ],
  };
}

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
