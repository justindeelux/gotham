import { http } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import { i18n } from "@/shared/i18n";
import { parseWith } from "@/shared/validation/parse";

import {
  createdTokenSchema,
  tokenListEnvelopeSchema,
} from "@/features/api-tokens/schemas/tokens";
import type {
  ApiToken,
  CreatedToken,
  TokenScope,
} from "@/features/api-tokens/schemas/tokens";

export async function listTokens(): Promise<ApiToken[]> {
  const response = await http.get<unknown>("/tokens");
  return parseWith(tokenListEnvelopeSchema, response.data, {
    context: "TokenList",
  }).tokens;
}

export async function createToken(
  name: string,
  scope: TokenScope,
): Promise<CreatedToken> {
  const response = await http.post<unknown>("/tokens", {
    name: name.trim(),
    scopes: [scope],
  });
  return parseWith(createdTokenSchema, response.data, {
    context: "TokenCreate",
  });
}

export async function rotateToken(id: string): Promise<CreatedToken> {
  const response = await http.post<unknown>(
    `/tokens/${encodeURIComponent(id)}/rotate`,
  );
  return parseWith(createdTokenSchema, response.data, {
    context: "TokenRotate",
  });
}

export async function revokeToken(id: string): Promise<void> {
  await http.delete(`/tokens/${encodeURIComponent(id)}`);
}

export function describeTokenError(error: unknown, fallback: string): string {
  if (!isApiError(error)) {
    return fallback;
  }
  if (error.status === 401) {
    return String(i18n.global.t("api-tokens.errors.sessionExpired"));
  }
  if (error.status === 403) {
    return (
      stripErrorPrefix(error.message) ||
      String(i18n.global.t("api-tokens.errors.forbidden"))
    );
  }
  if (error.status === 404) {
    return (
      stripErrorPrefix(error.message) ||
      String(i18n.global.t("api-tokens.errors.notFound"))
    );
  }
  return stripErrorPrefix(error.message) || fallback;
}
