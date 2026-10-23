import { z } from "zod";

import type { ChannelForm } from "@/features/notifications/utils/channelHelpers";

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

export function canSubmitChannel(
  form: Pick<
    ChannelForm,
    "name" | "events" | "resourceType" | "resourceId"
  >,
): boolean {
  return channelSubmitSchema.safeParse(form).success;
}
