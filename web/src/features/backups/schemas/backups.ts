import { z } from "zod";

import type { StorageProviderKind } from "@/features/backups/api/backups";
import { requiredString } from "@/shared/validation/primitives";

export const backupMessages = {
  cronRequired: "databases.validation.cronRequired",
  providerNameRequired: "databases.validation.targetNameRequired",
  s3LocationRequired: "databases.validation.s3LocationRequired",
  s3KeysRequired: "databases.validation.s3KeysRequired",
} as const;

export const cronSchema = requiredString(backupMessages.cronRequired);

export function isCronPresent(value: unknown): boolean {
  return cronSchema.safeParse(value).success;
}

export const providerNameSchema = requiredString(
  backupMessages.providerNameRequired,
);

export const providerEndpointSchema = requiredString(
  backupMessages.s3LocationRequired,
);

export const providerBucketSchema = requiredString(
  backupMessages.s3LocationRequired,
);

export const providerKeySchema = z
  .string({
    error: backupMessages.s3KeysRequired,
  })
  .min(1, backupMessages.s3KeysRequired);

export interface ProviderDraft {
  name: string;
  kind: StorageProviderKind;
  endpoint: string;
  bucket: string;
  accessKey: string;
  secretKey: string;
  isNew: boolean;
}

export function validateProviderForm(draft: ProviderDraft): string | null {
  if (!providerNameSchema.safeParse(draft.name).success) {
    return backupMessages.providerNameRequired;
  }
  if (draft.kind === "s3") {
    if (
      !providerEndpointSchema.safeParse(draft.endpoint).success ||
      !providerBucketSchema.safeParse(draft.bucket).success
    ) {
      return backupMessages.s3LocationRequired;
    }
    if (
      draft.isNew &&
      (!providerKeySchema.safeParse(draft.accessKey).success ||
        !providerKeySchema.safeParse(draft.secretKey).success)
    ) {
      return backupMessages.s3KeysRequired;
    }
  }
  return null;
}
