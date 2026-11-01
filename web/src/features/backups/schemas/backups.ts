import { z } from "zod";

import { requiredString } from "@/shared/validation/primitives";

export const backupMessages = {
  cronRequired: "databases.validation.cronRequired",
  keepLastInvalid: "backups.schedules.keepLastInvalid",
} as const;

export const keepLastSchema = z
  .number({
    error: backupMessages.keepLastInvalid,
  })
  .int(backupMessages.keepLastInvalid)
  .min(1, backupMessages.keepLastInvalid)
  .max(365, backupMessages.keepLastInvalid);

export function isKeepLastValid(value: number | null): boolean {
  return value === null || keepLastSchema.safeParse(value).success;
}

export const cronSchema = requiredString(backupMessages.cronRequired);

export function isCronPresent(value: unknown): boolean {
  return cronSchema.safeParse(value).success;
}
