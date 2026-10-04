import { z } from "zod";

import type { ChannelForm } from "@/features/notifications/utils/channelHelpers";
import { parseRecipients } from "@/features/notifications/utils/channelHelpers";

/**
 * Notification channel schemas (V8). canSubmit today is boolean-only (no
 * toast strings: the submit button is disabled, server errors surface via
 * describeChannelError), so these messages are never rendered; they exist so
 * a future ruleFrom wiring has catalogued strings. The events string reuses
 * the existing inline hint text verbatim.
 */
export const channelMessages = {
  nameRequired: "Channel name is required.",
  eventsRequired: "Select at least one event.",
  resourceRequired: "Select a resource for this scope.",
} as const;

/** Channel scope as stored: empty means team-wide. */
const channelScopeSchema = z.union([
  z.literal(""),
  z.literal("application"),
  z.literal("database"),
]);

/**
 * channelSubmitSchema replaces canSubmit: a trimmed non-empty name, at
 * least one event, and a picked resource whenever the scope is not
 * team-wide. Unknown keys (kind, config fields) are stripped, never failed.
 */
export const channelSubmitSchema = z
  .object({
    name: z.string().trim().min(1, channelMessages.nameRequired),
    events: z.array(z.string()).min(1, channelMessages.eventsRequired),
    resourceType: channelScopeSchema,
    resourceId: z.string(),
  })
  .superRefine((value, ctx) => {
    if (value.resourceType !== "" && value.resourceId === "") {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: channelMessages.resourceRequired,
        path: ["resourceId"],
      });
    }
  });

/** ChannelSubmitInput is the gating subset of the dialog draft. */
export type ChannelSubmitInput = z.infer<typeof channelSubmitSchema>;

/**
 * canSubmitChannel is the single source for the dialog submit disabled
 * state. It accepts the full ChannelForm (extra keys are ignored).
 */
export function canSubmitChannel(
  form: Pick<
    ChannelForm,
    "name" | "events" | "resourceType" | "resourceId"
  >,
): boolean {
  return channelSubmitSchema.safeParse(form).success;
}

/**
 * recipientsSchema models parseRecipients at the boundary: split on
 * /[\s,]+/, trim, drop empties. The transform delegates to the same
 * function buildConfig uses, so there is one splitter implementation.
 */
export const recipientsSchema = z.string().transform((raw) => parseRecipients(raw));

/** Recipients is the parsed address list. */
export type Recipients = z.infer<typeof recipientsSchema>;
