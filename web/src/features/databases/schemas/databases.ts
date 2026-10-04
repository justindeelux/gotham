import { z } from "zod";

import type { BackupTargetKind } from "@/features/databases/api/backups";
import { portSchema, requiredString } from "@/shared/validation/primitives";

/**
 * Database form schemas (V7). Every message below is copied verbatim from
 * the toast the hand-written guard showed, so the swap has zero
 * user-visible diff. None of these sites uses NForm rules today (submit
 * guards toast + disabled states), so the schemas drive the same guards
 * and disabled states instead of Naive rules; adding NForm rules would
 * change validation timing.
 */
export const databaseMessages = {
  nameRule: "Name must be 1-63 characters of letters, digits, ., _ or -.",
  cronRequired: "Cron expression is required, e.g. 0 2 * * *.",
  targetNameRequired: "Target name is required.",
  s3LocationRequired: "Endpoint and bucket are required for an S3 target.",
  s3KeysRequired: "Access key and secret key are required for a new S3 target.",
} as const;

/** Backend name rule from internal/databases/service.go (namePattern). */
export const DATABASE_NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/;

/**
 * databaseNameSchema replaces isValidDatabaseName: the trimmed value must
 * match the backend pattern. Every absent/invalid value reports the single
 * rename toast string.
 */
export const databaseNameSchema = z
  .string({
    required_error: databaseMessages.nameRule,
    invalid_type_error: databaseMessages.nameRule,
  })
  .trim()
  .regex(DATABASE_NAME_PATTERN, databaseMessages.nameRule);

/** isDatabaseNameValid is the single source for rename + wizard gating. */
export function isDatabaseNameValid(value: unknown): boolean {
  return databaseNameSchema.safeParse(value).success;
}

/**
 * cronSchema replaces the trim-and-empty guard in handleCreateSchedule.
 * Non-empty only: there is deliberately NO client grammar check (the
 * numeric-only grammar lives server-side; see docs/library-audit.md F3).
 */
export const cronSchema = requiredString(databaseMessages.cronRequired);

/** isCronPresent drives the schedule submit disabled state. */
export function isCronPresent(value: unknown): boolean {
  return cronSchema.safeParse(value).success;
}

/** targetNameSchema replaces the trim-and-empty target name guard. */
export const targetNameSchema = requiredString(
  databaseMessages.targetNameRequired,
);

/** S3 endpoint/bucket share one message: both must be non-empty. */
export const targetEndpointSchema = requiredString(
  databaseMessages.s3LocationRequired,
);

/** targetBucketSchema shares the endpoint/bucket message. */
export const targetBucketSchema = requiredString(
  databaseMessages.s3LocationRequired,
);

/**
 * targetKeySchema guards both key fields on a new S3 target. It deliberately
 * does NOT trim: the old guard compared the raw value (`=== ""`), so a
 * blank-space key passed. Trimming here would reject input the old code
 * accepted.
 */
export const targetKeySchema = z
  .string({
    required_error: databaseMessages.s3KeysRequired,
    invalid_type_error: databaseMessages.s3KeysRequired,
  })
  .min(1, databaseMessages.s3KeysRequired);

/** BackupTargetDraft is the target editor state the guard reads. */
export interface BackupTargetDraft {
  name: string;
  kind: BackupTargetKind;
  endpoint: string;
  bucket: string;
  accessKey: string;
  secretKey: string;
  /** True while creating (targetEditingId === null); keys stay optional on edit. */
  isNew: boolean;
}

/**
 * validateTargetForm replaces the sequential guards in handleSaveTarget and
 * returns the first failure message, or null when the draft may be sent.
 * Check order matches the old code: name, then S3 location, then new keys.
 */
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

/** WizardConfigureDraft is the configure-step state the gate reads. */
export interface WizardConfigureDraft {
  name: string;
  exposePublic: boolean;
  publicPort: number | null;
}

/**
 * isWizardConfigureValid replaces configureValid: the backend name rule,
 * plus the 1-65535 integer port (shared portSchema: NInputNumber emits null
 * when cleared and NaN for unparseable input, both fail) only when a public
 * port is exposed.
 */
export function isWizardConfigureValid(draft: WizardConfigureDraft): boolean {
  if (!isDatabaseNameValid(draft.name)) {
    return false;
  }
  if (draft.exposePublic) {
    return portSchema.safeParse(draft.publicPort).success;
  }
  return true;
}
