import { z } from "zod";

import type { ChannelForm } from "@/features/notifications/utils/channelHelpers";

/**
 * Notification channel submit schema (V8). canSubmit today is boolean-only:
 * the submit button is disabled and server errors surface via
 * describeChannelError, so no message here is ever rendered. Messages stay
 * zod defaults on purpose; add catalogued strings only if a UI starts
 * rendering them. resourceType stays z.string for strict parity: the old
 * check accepted any string and only required a resource id when non-empty.
 */
export const channelSubmitSchema = z
  .object({
    name: z.string().trim().min(1),
    events: z.array(z.string()).min(1),
    resourceType: z.string(),
    resourceId: z.string(),
  })
  .superRefine((value, ctx) => {
    if (value.resourceType !== "" && value.resourceId === "") {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["resourceId"],
      });
    }
  });

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
