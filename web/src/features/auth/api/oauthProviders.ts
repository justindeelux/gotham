import { http } from "@/shared/api/http";

import { enabledOAuthProvidersSchema } from "@/features/auth/schemas/oauthProviders";

export type OAuthProviderName = "github" | "google";

export const oauthProviderNames: OAuthProviderName[] = ["github", "google"];

// Disabled providers answer with an empty list; a failed request must not block
// the email and password form, so it reads as "no provider" too.
export async function getEnabledOAuthProviders(): Promise<OAuthProviderName[]> {
  const response = await http.get("/auth/oauth/providers");
  const parsed = enabledOAuthProvidersSchema.safeParse(response.data);
  if (!parsed.success) {
    return [];
  }
  return oauthProviderNames.filter((name) => parsed.data.providers.includes(name));
}
