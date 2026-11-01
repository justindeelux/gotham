import { isApiError, stripErrorPrefix } from "@/features/servers";
import {
  storageCheckSchema,
  storageEnvelopeSchema,
  storageKindSchema,
  storageListEnvelopeSchema,
} from "@/features/storages/schemas/storages";
import type { Storage, StorageCheck, StorageKind } from "@/features/storages/schemas/storages";
import { toProviderBody } from "@/features/storages/utils/providerBody";
import { http } from "@/shared/api/http";
import { i18n } from "@/shared/i18n";
import { parseWith } from "@/shared/validation/parse";

export interface CreateStorageInput {
  name: string;
  kind: StorageKind;
  endpoint?: string;
  region?: string;
  bucket?: string;
  prefix?: string;
  access_key?: string;
  secret_key?: string;
}

export interface UpdateStorageInput extends CreateStorageInput {}

export async function listStorages(): Promise<Storage[]> {
  const response = await http.get<unknown>("/storages");
  return parseWith(storageListEnvelopeSchema, response.data, {
    context: "StorageList",
  }).storages;
}

export async function createStorage(input: CreateStorageInput): Promise<Storage> {
  const response = await http.post<unknown>("/storages", toProviderBody(input));
  return parseWith(storageEnvelopeSchema, response.data, {
    context: "Storage",
  }).storage;
}

export async function updateStorage(
  storageId: string,
  input: UpdateStorageInput,
): Promise<Storage> {
  const response = await http.patch<unknown>(
    `/storages/${storageId}`,
    toProviderBody(input),
  );
  return parseWith(storageEnvelopeSchema, response.data, {
    context: "Storage",
  }).storage;
}

export async function deleteStorage(
  storageId: string,
  confirm = false,
): Promise<void> {
  await http.delete(`/storages/${storageId}`, {
    params: confirm ? { confirm: "true" } : undefined,
  });
}

export async function testStorage(storageId: string): Promise<StorageCheck> {
  const response = await http.post<unknown>(`/storages/${storageId}/test`, {});
  return parseWith(storageCheckSchema, response.data, {
    context: "StorageCheck",
  }).check;
}

export function isStorageKind(value: string): value is StorageKind {
  return storageKindSchema.safeParse(value).success;
}

export function describeStorageError(error: unknown): string {
  const t = (key: string): string => String(i18n.global.t(key));
  const withDetail = (summaryKey: string, raw: string): string =>
    raw === ""
      ? t(summaryKey)
      : String(
          i18n.global.t("databases.errors.withDetail", {
            summary: t(summaryKey),
            detail: raw,
          }),
        );
  if (isApiError(error)) {
    const raw = stripErrorPrefix(error.message);
    if (error.status === 400) {
      return withDetail("storages.errors.invalidRequest", raw);
    }
    if (error.status === 401) {
      return t("databases.errors.sessionExpired");
    }
    if (error.status === 403) {
      return t("storages.errors.forbidden");
    }
    if (error.status === 404) {
      return t("storages.errors.notFound");
    }
    if (error.status === 409) {
      return withDetail("storages.errors.conflict", raw);
    }
    if (error.status === 502) {
      return withDetail("storages.errors.agentUnreachable", raw);
    }
    if (error.status === 503) {
      return t("storages.errors.unavailable");
    }
    return withDetail("common.errors.requestFailed", raw);
  }
  if (error instanceof Error) {
    return withDetail("common.errors.unexpected", stripErrorPrefix(error.message));
  }
  return t("common.errors.unexpected");
}
