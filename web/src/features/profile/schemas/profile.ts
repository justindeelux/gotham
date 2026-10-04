import type { FormRules } from "naive-ui";
import { z } from "zod";

import { confirmPasswordSchema, registerPasswordSchema } from "@/features/auth";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

/**
 * Profile schemas (JUS-27). One message catalog seeds every string rendered
 * by the profile forms; the new/confirm password rules are reused from the
 * auth schemas (same policy message as register, per the API contract) and
 * are not duplicated here.
 */
export const profileMessages = {
  displayNameLength: "Display name must be 1-64 characters",
  currentPasswordRequired: "Current password is required",
  sessionsLoadFailed: "Could not load sessions. Try again.",
  sessionEndFailed: "Could not sign out that session. Try again.",
  revokeOthersFailed: "Could not sign out the other sessions. Try again.",
  sessionsListStale: "Signed out, but the session list may be out of date.",
  sessionsSignedOut: "Other devices were signed out.",
  sessionSignedOut: "Session signed out.",
  sessionsSignedOutHere: "Signed out on this device.",
  needsReauth:
    "Your sign-in predates session management. Sign in again to manage other sessions.",
  sessionsIntro:
    "Every device signed in to your account. Ending a session signs that device out; ending this device signs you out here.",
  sessionsEmpty: "No active sessions.",
  actionRetry: "Retry",
  signInAgain: "Sign in again",
} as const;

/**
 * displayNameFieldSchema backs the display-name field. The value is trimmed;
 * blank input is valid and clears the name (sent as null), anything else
 * must be 1-64 characters after trimming.
 */
export const displayNameFieldSchema = z
  .string({
    required_error: profileMessages.displayNameLength,
    invalid_type_error: profileMessages.displayNameLength,
  })
  .trim()
  .max(64, profileMessages.displayNameLength);

/** currentPasswordSchema is required-only (whitespace passes, as on login). */
export const currentPasswordSchema = z
  .string({
    required_error: profileMessages.currentPasswordRequired,
    invalid_type_error: profileMessages.currentPasswordRequired,
  })
  .min(1, profileMessages.currentPasswordRequired);

/**
 * displayNameRules builds the DisplayNameForm NForm rules: one schema-backed
 * entry with no required mark, because blank is valid and clears the name.
 */
export function displayNameRules(): FormRules {
  return {
    displayName: [
      { ...ruleFrom(displayNameFieldSchema), trigger: ["input", "blur"] },
    ],
  };
}

/**
 * changePasswordRules builds the ChangePasswordForm NForm rules. The current
 * password entry is present only when the account has one; the new password
 * reuses the register policy and the confirmation check reads the live new
 * password through a reader (not a snapshot).
 */
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

/** userEnvelopeShape mirrors the User transport for envelope validation. */
const userEnvelopeShape = z.object({
  id: z.string(),
  email: z.string(),
  avatar: z.string().nullable().optional(),
  role: z.string().optional(),
  created_at: z.string(),
  display_name: z.string().nullable().optional(),
  has_password: z.boolean().optional(),
  is_platform_admin: z.boolean().optional(),
});

/** meEnvelopeSchema validates the GET/PATCH /me {"user"} envelope. */
export const meEnvelopeSchema = z.object({ user: userEnvelopeShape });

/** passwordChangeEnvelopeSchema validates the POST /me/password AuthResult. */
export const passwordChangeEnvelopeSchema = z.object({
  user: userEnvelopeShape,
  access_token: z.string(),
  token_type: z.string(),
  expires_in: z.number(),
  refresh_token: z.string(),
});
