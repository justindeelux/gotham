import { z } from "zod";

import type { BackupTargetKind } from "@/features/databases/api/backups";
import { portSchema, requiredString } from "@/shared/validation/primitives";

export const databaseMessages = {
  nameRule: "databases.validation.nameRule",
  cronRequired: "databases.validation.cronRequired",
  targetNameRequired: "databases.validation.targetNameRequired",
  s3LocationRequired: "databases.validation.s3LocationRequired",
  s3KeysRequired: "databases.validation.s3KeysRequired",
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

export const cronSchema = requiredString(databaseMessages.cronRequired);

export function isCronPresent(value: unknown): boolean {
  return cronSchema.safeParse(value).success;
}

export const targetNameSchema = requiredString(
  databaseMessages.targetNameRequired,
);

export const targetEndpointSchema = requiredString(
  databaseMessages.s3LocationRequired,
);

export const targetBucketSchema = requiredString(
  databaseMessages.s3LocationRequired,
);

export const targetKeySchema = z
  .string({
    error: databaseMessages.s3KeysRequired,
  })
  .min(1, databaseMessages.s3KeysRequired);

export interface BackupTargetDraft {
  name: string;
  kind: BackupTargetKind;
  endpoint: string;
  bucket: string;
  accessKey: string;
  secretKey: string;
  isNew: boolean;
}

export function validateTargetForm(draft: BackupTargetDraft): string | null {
  if (!targetNameSchema.safeParse(draft.name).success) {
    return databaseMessages.targetNameRequired;
  }
  if (draft.kind === "s3") {
    if (
      !targetEndpointSchema.safeParse(draft.endpoint).success ||
      !targetBucketSchema.safeParse(draft.bucket).success
    ) {
      return databaseMessages.s3LocationRequired;
    }
    if (
      draft.isNew &&
      (!targetKeySchema.safeParse(draft.accessKey).success ||
        !targetKeySchema.safeParse(draft.secretKey).success)
    ) {
      return databaseMessages.s3KeysRequired;
    }
  }
  return null;
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
