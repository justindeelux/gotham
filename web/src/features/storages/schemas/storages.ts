import { z } from "zod";

import { requiredString } from "@/shared/validation/primitives";

export const storageKindSchema = z.enum(["s3", "local"]);

export type StorageKind = z.infer<typeof storageKindSchema>;

export const storageSchema = z.object({
  id: z.string(),
  name: z.string(),
  kind: storageKindSchema,
  endpoint: z.string().optional(),
  region: z.string().optional(),
  bucket: z.string().optional(),
  prefix: z.string().optional(),
  builtin: z.boolean(),
  has_credentials: z.boolean(),
  backup_count: z.number(),
  total_size: z.number(),
  free_space: z.number().optional(),
  total_space: z.number().optional(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const storageListEnvelopeSchema = z.object({
  storages: z.array(storageSchema),
});

export const storageEnvelopeSchema = z.object({
  storage: storageSchema,
});

export const storageCheckSchema = z.object({
  check: z.object({
    ok: z.boolean(),
    message: z.string(),
  }),
});

export type Storage = z.infer<typeof storageSchema>;
export type StorageCheck = z.infer<typeof storageCheckSchema>["check"];

export const storageMessages = {
  nameRequired: "storages.validation.nameRequired",
  s3LocationRequired: "storages.validation.s3LocationRequired",
  s3KeysRequired: "storages.validation.s3KeysRequired",
} as const;

export const storageNameSchema = requiredString(storageMessages.nameRequired);
export const storageEndpointSchema = requiredString(
  storageMessages.s3LocationRequired,
);
export const storageBucketSchema = requiredString(
  storageMessages.s3LocationRequired,
);
export const storageKeySchema = z
  .string({ error: storageMessages.s3KeysRequired })
  .min(1, storageMessages.s3KeysRequired);

export interface StorageDraft {
  name: string;
  kind: StorageKind;
  endpoint: string;
  bucket: string;
  accessKey: string;
  secretKey: string;
  isNew: boolean;
}

// One schema for the dialog keeps the checks in step with the API contract:
// an S3 destination needs a location and, when created, both keys.
export const storageFormSchema = z
  .object({
    name: storageNameSchema,
    kind: storageKindSchema,
    endpoint: z.string(),
    bucket: z.string(),
    accessKey: z.string(),
    secretKey: z.string(),
    isNew: z.boolean(),
  })
  .superRefine((draft, ctx) => {
    if (draft.kind !== "s3") {
      return;
    }
    if (!storageEndpointSchema.safeParse(draft.endpoint).success) {
      ctx.addIssue({
        code: "custom",
        path: ["endpoint"],
        message: storageMessages.s3LocationRequired,
      });
    }
    if (!storageBucketSchema.safeParse(draft.bucket).success) {
      ctx.addIssue({
        code: "custom",
        path: ["bucket"],
        message: storageMessages.s3LocationRequired,
      });
    }
    if (
      draft.isNew &&
      (!storageKeySchema.safeParse(draft.accessKey).success ||
        !storageKeySchema.safeParse(draft.secretKey).success)
    ) {
      ctx.addIssue({
        code: "custom",
        path: ["accessKey"],
        message: storageMessages.s3KeysRequired,
      });
    }
  });

export function firstStorageFormIssue(draft: StorageDraft): string | null {
  const result = storageFormSchema.safeParse(draft);
  return result.success ? null : (result.error.issues[0]?.message ?? null);
}
