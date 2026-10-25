import { z } from "zod";

export const tokenScopeSchema = z.enum(["read", "deploy", "admin"]);

export const apiTokenSchema = z.object({
  id: z.string(),
  name: z.string(),
  scopes: z.array(tokenScopeSchema),
  last_used_at: z.string().nullable(),
  revoked_at: z.string().nullable(),
  created_at: z.string(),
});

export const tokenListEnvelopeSchema = z.object({
  tokens: z.array(apiTokenSchema),
});

export const createdTokenSchema = z.object({
  id: z.string(),
  name: z.string(),
  scopes: z.array(tokenScopeSchema),
  token: z.string(),
  created_at: z.string(),
});

export const tokenMessages = {
  nameRequired: "api-tokens.validation.nameRequired",
  nameMaxLength: "api-tokens.validation.nameMaxLength",
} as const;

export const tokenNameSchema = z
  .string()
  .trim()
  .min(1, tokenMessages.nameRequired)
  .max(64, tokenMessages.nameMaxLength);

export const createTokenFormSchema = z.object({
  name: tokenNameSchema,
  scope: tokenScopeSchema,
});

export type TokenScope = z.infer<typeof tokenScopeSchema>;
export type ApiToken = z.infer<typeof apiTokenSchema>;
export type CreatedToken = z.infer<typeof createdTokenSchema>;
