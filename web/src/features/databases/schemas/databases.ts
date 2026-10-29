import { z } from "zod";

import { portSchema } from "@/shared/validation/primitives";

export const databaseMessages = {
  nameRule: "databases.validation.nameRule",
} as const;

export const DATABASE_NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/;

export const databaseNameSchema = z
  .string({
    error: databaseMessages.nameRule,
  })
  .trim()
  .regex(DATABASE_NAME_PATTERN, databaseMessages.nameRule);

export function isDatabaseNameValid(value: unknown): boolean {
  return databaseNameSchema.safeParse(value).success;
}

export interface WizardConfigureDraft {
  name: string;
  exposePublic: boolean;
  publicPort: number | null;
}

export function isWizardConfigureValid(draft: WizardConfigureDraft): boolean {
  if (!isDatabaseNameValid(draft.name)) {
    return false;
  }
  if (draft.exposePublic) {
    return portSchema.safeParse(draft.publicPort).success;
  }
  return true;
}
