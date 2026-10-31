import { conflictDetail, isApiError, stripErrorPrefix } from "@/features/servers";
import { listServers } from "@/features/servers/api/servers";
import type { Server } from "@/features/servers";
import { http } from "@/shared/api/http";
import { i18n } from "@/shared/i18n";
import { parseWith } from "@/shared/validation/parse";

import {
  privateKeyListEnvelopeSchema,
  privateKeyRevealSchema,
  privateKeySchema,
} from "@/features/private-keys/schemas/keys";
import type { PrivateKey, PrivateKeyType } from "@/features/private-keys/schemas/keys";

export interface CreatePrivateKeyInput {
  name: string;
  description: string;
  private_key: string;
  public_key: string;
}

export interface GeneratePrivateKeyInput {
  name: string;
  description: string;
  type: PrivateKeyType;
}

export interface UpdatePrivateKeyInput {
  name: string;
  description: string;
}

export async function listPrivateKeys(): Promise<PrivateKey[]> {
  const response = await http.get<unknown>("/private-keys");
  return parseWith(privateKeyListEnvelopeSchema, response.data, {
    context: "PrivateKeyList",
  }).private_keys;
}

export async function createPrivateKey(
  input: CreatePrivateKeyInput,
): Promise<PrivateKey> {
  const response = await http.post<unknown>("/private-keys", {
    ...input,
    name: input.name.trim(),
    description: input.description.trim(),
    private_key: input.private_key.trim(),
    public_key: input.public_key.trim(),
  });
  return parseWith(privateKeySchema, response.data, {
    context: "PrivateKeyCreate",
  });
}

export async function generatePrivateKey(
  input: GeneratePrivateKeyInput,
): Promise<PrivateKey> {
  const response = await http.post<unknown>("/private-keys/generate", {
    ...input,
    name: input.name.trim(),
    description: input.description.trim(),
  });
  return parseWith(privateKeySchema, response.data, {
    context: "PrivateKeyGenerate",
  });
}

export async function updatePrivateKey(
  id: string,
  input: UpdatePrivateKeyInput,
): Promise<PrivateKey> {
  const response = await http.patch<unknown>(
    `/private-keys/${encodeURIComponent(id)}`,
    {
      name: input.name.trim(),
      description: input.description.trim(),
    },
  );
  return parseWith(privateKeySchema, response.data, {
    context: "PrivateKeyUpdate",
  });
}

export async function deletePrivateKey(id: string): Promise<void> {
  await http.delete(`/private-keys/${encodeURIComponent(id)}`);
}

export async function revealPrivateKey(id: string): Promise<string> {
  const response = await http.post<unknown>(
    `/private-keys/${encodeURIComponent(id)}/reveal`,
  );
  return parseWith(privateKeyRevealSchema, response.data, {
    context: "PrivateKeyReveal",
  }).private_key;
}

export async function listServersUsingKeys(): Promise<Server[]> {
  return listServers();
}

export function describePrivateKeyError(
  error: unknown,
  fallback: string,
): string {
  if (!isApiError(error)) {
    return fallback;
  }
  if (error.status === 401) {
    return String(i18n.global.t("private-keys.errors.sessionExpired"));
  }
  if (error.status === 403) {
    return (
      stripErrorPrefix(error.message) ||
      String(i18n.global.t("private-keys.errors.forbidden"))
    );
  }
  if (error.status === 404) {
    return (
      stripErrorPrefix(error.message) ||
      String(i18n.global.t("private-keys.errors.notFound"))
    );
  }
  if (error.status === 409) {
    return (
      conflictDetail(stripErrorPrefix(error.message)) ||
      String(i18n.global.t("private-keys.errors.inUse"))
    );
  }
  return stripErrorPrefix(error.message) || fallback;
}
