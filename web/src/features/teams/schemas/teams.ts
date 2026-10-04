import { z } from "zod";

/**
 * Team form schemas (V9 sweep). Both dialogs gate their submit button on a
 * trimmed non-empty value and render no client message (an Enter-key submit
 * with an empty value reaches the server and its error renders in the dialog
 * alert), so messages stay zod defaults on purpose; add catalogued strings
 * only if a UI starts rendering them. The invite keeps the current acceptance
 * of any non-empty string: there is deliberately NO email-format check
 * client-side (adding one would reject input the current form accepts).
 */
export const teamNameSchema: z.ZodString = z.string().trim().min(1);

/** inviteEmailSchema replaces the trim-and-empty invite gating. */
export const inviteEmailSchema: z.ZodString = z.string().trim().min(1);

/** isTeamNameValid is the single source for the create/rename disabled state. */
export function isTeamNameValid(value: unknown): boolean {
  return teamNameSchema.safeParse(value).success;
}

/** isInviteEmailValid is the single source for the invite disabled state. */
export function isInviteEmailValid(value: unknown): boolean {
  return inviteEmailSchema.safeParse(value).success;
}
