import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import {
  oauthProviderInfoSchema,
  oauthProviderListSchema,
} from "@/features/instance-settings/schemas/oauthProviders";

export type OAuthProviderName = "github" | "google";

export const oauthProviderNames: OAuthProviderName[] = ["github", "google"];

export interface OAuthProviderInfo {
  name: string;
  enabled: boolean;
  client_id: string;
  has_secret: boolean;
}

export interface OAuthProviderUpdate {
  client_id: string;
  client_secret?: string;
  enabled: boolean;
}

export async function getOAuthProviders(): Promise<OAuthProviderInfo[]> {
  const response = await http.get("/instance/oauth-providers");
  return parseWith(oauthProviderListSchema, response.data, {
    context: "OAuthProviderList",
  }).providers;
}

export async function saveOAuthProvider(
  name: string,
  input: OAuthProviderUpdate,
): Promise<OAuthProviderInfo> {
  const response = await http.put(`/instance/oauth-providers/${name}`, input);
  return parseWith(oauthProviderInfoSchema, response.data, {
    context: "OAuthProvider",
  });
}
