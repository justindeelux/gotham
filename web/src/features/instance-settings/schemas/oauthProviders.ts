import { z } from "zod";

const m = (key: string): string => `instance-settings.oauth.validation.${key}`;

export const oauthClientIdSchema = z
  .string()
  .trim()
  .min(1, m("clientId"))
  .max(255, m("clientId"));

export const oauthClientSecretSchema = z
  .string()
  .trim()
  .min(1, m("clientSecret"))
  .max(255, m("clientSecret"));

export const oauthProviderInfoSchema = z.object({
  name: z.string(),
  enabled: z.boolean(),
  client_id: z.string(),
  has_secret: z.boolean(),
});

export const oauthProviderListSchema = z.object({
  providers: z.array(oauthProviderInfoSchema),
});
