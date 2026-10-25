import type { FormRules } from "naive-ui";
import { z } from "zod";

import { confirmPasswordSchema, registerPasswordSchema } from "@/features/auth";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

export const profileMessages = {
  displayNameLength: "profile.validation.displayNameLength",
  currentPasswordRequired: "profile.validation.currentPasswordRequired",
  sessionsLoadFailed: "profile.sessions.loadFailed",
  sessionEndFailed: "profile.sessions.endFailed",
  revokeOthersFailed: "profile.sessions.revokeOthersFailed",
  sessionsListStale: "profile.sessions.listStale",
  sessionsSignedOut: "profile.sessions.signedOut",
  sessionSignedOut: "profile.sessions.sessionSignedOut",
  sessionsSignedOutHere: "profile.sessions.signedOutHere",
  needsReauth: "profile.sessions.needsReauth",
  sessionsIntro: "profile.sessions.intro",
  sessionsEmpty: "profile.sessions.empty",
  actionRetry: "profile.sessions.retry",
  signInAgain: "profile.sessions.signInAgain",
} as const;

export const displayNameFieldSchema = z
  .string({
    error: profileMessages.displayNameLength,
  })
  .trim()
  .max(64, profileMessages.displayNameLength);

export const currentPasswordSchema = z
  .string({
    error: profileMessages.currentPasswordRequired,
  })
  .min(1, profileMessages.currentPasswordRequired);

export function displayNameRules(): FormRules {
  return {
    displayName: [
      { ...ruleFrom(displayNameFieldSchema), trigger: ["input", "blur"] },
    ],
  };
}

export function changePasswordRules(
  hasPassword: boolean,
  password: () => string,
): FormRules {
  return {
    ...(hasPassword
      ? {
          currentPassword: [
            {
              ...ruleFrom(currentPasswordSchema, { required: true }),
              trigger: ["input", "blur"],
            },
          ],
        }
      : {}),
    newPassword: [
      { ...ruleFrom(registerPasswordSchema, { required: true }), trigger: ["input", "blur"] },
    ],
    confirmPassword: [
      {
        ...ruleFrom(confirmPasswordSchema(password), { required: true }),
        trigger: ["input", "blur"],
      },
    ],
  };
}

const userEnvelopeShape = z.object({
  id: z.string(),
  email: z.string(),
  avatar: z.string().nullable().optional(),
  role: z.string().optional(),
  created_at: z.string(),
  display_name: z.string().nullable().optional(),
  has_password: z.boolean().optional(),
});

export const meEnvelopeSchema = z.object({ user: userEnvelopeShape });

export const passwordChangeEnvelopeSchema = z.object({
  user: userEnvelopeShape,
  access_token: z.string(),
  token_type: z.string(),
  expires_in: z.number(),
  refresh_token: z.string(),
});
