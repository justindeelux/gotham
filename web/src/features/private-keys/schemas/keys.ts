import { z } from "zod";

import { requiredString } from "@/shared/validation/primitives";

export const privateKeySchema = z.object({
  id: z.string(),
  name: z.string(),
  type: z.string(),
  description: z.string().nullable(),
  public_key: z.string(),
  created_at: z.string(),
});

export const privateKeyListEnvelopeSchema = z.object({
  private_keys: z.array(privateKeySchema),
});

export const privateKeyRevealSchema = z.object({
  private_key: z.string(),
});

export const privateKeyTypeSchema = z.enum(["ed25519", "rsa", "ecdsa"]);

export const privateKeyMessages = {
  nameRequired: "private-keys.validation.nameRequired",
  nameMaxLength: "private-keys.validation.nameMaxLength",
  descriptionMaxLength: "private-keys.validation.descriptionMaxLength",
  privateKeyRequired: "private-keys.validation.privateKeyRequired",
} as const;

export const privateKeyNameSchema = z
  .string()
  .trim()
  .min(1, privateKeyMessages.nameRequired)
  .max(64, privateKeyMessages.nameMaxLength);

export const privateKeyDescriptionSchema = z
  .string()
  .max(500, privateKeyMessages.descriptionMaxLength);

export const generatePrivateKeyFormSchema = z.object({
  name: privateKeyNameSchema,
  description: privateKeyDescriptionSchema,
  type: privateKeyTypeSchema,
});

export const importPrivateKeyFormSchema = z.object({
  name: privateKeyNameSchema,
  description: privateKeyDescriptionSchema,
  private_key: requiredString(privateKeyMessages.privateKeyRequired),
  public_key: z.string(),
});

export const editPrivateKeyFormSchema = z.object({
  name: privateKeyNameSchema,
  description: privateKeyDescriptionSchema,
});

export type PrivateKey = z.infer<typeof privateKeySchema>;
export type PrivateKeyType = z.infer<typeof privateKeyTypeSchema>;
