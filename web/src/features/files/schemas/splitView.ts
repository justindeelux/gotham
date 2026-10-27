import { z } from "zod";

export const FILES_SPLIT_VERSION = 1 as const;

export const filesSplitSchema = z.object({
  version: z.literal(FILES_SPLIT_VERSION),
  enabled: z.boolean(),
  secondaryServerId: z.string().max(128),
});
export type FilesSplitState = z.infer<typeof filesSplitSchema>;
