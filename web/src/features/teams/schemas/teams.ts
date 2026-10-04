import { z } from "zod";

import { requiredString } from "@/shared/validation/primitives";

/**
 * Team form schemas (V9 sweep). Both dialogs gate their submit button on a
 * trimmed non-empty value and show NO client message (the button is simply
 * disabled; an Enter-key submit with an empty value reaches the server and
 * its error renders in the dialog alert). The catalog strings are therefore
 * never rendered today; they exist so a future ruleFrom wiring has them.
 * The invite keeps the current acceptance of any non-empty string: there is
 * deliberately NO email-format check client-side (adding one would reject
 * input the current form accepts).
 */
export const teamMessages = {
  nameRequired: "Team name is required.",
  emailRequired: "Invite email is required.",
} as const;

/** teamNameSchema replaces the trim-and-empty create/rename gating. */
export const teamNameSchema: z.ZodString = requiredString(
  teamMessages.nameRequired,
);

/** inviteEmailSchema replaces the trim-and-empty invite gating. */
export const inviteEmailSchema: z.ZodString = requiredString(
  teamMessages.emailRequired,
);

/** isTeamNameValid is the single source for the create/rename disabled state. */
export function isTeamNameValid(value: unknown): boolean {
  return teamNameSchema.safeParse(value).success;
}

/** isInviteEmailValid is the single source for the invite disabled state. */
export function isInviteEmailValid(value: unknown): boolean {
  return inviteEmailSchema.safeParse(value).success;
}
