import { useMessage } from "naive-ui";
import { ref } from "vue";
import type { InjectionKey } from "vue";

import { describeBackupError } from "@/features/backups/api/backups";
import type { StorageProviderKind } from "@/features/backups/api/backups";
import {
  backupMessages,
  validateProviderForm,
} from "@/features/backups/schemas/backups";
import { useBackupsStore } from "@/features/backups/stores/backups";
import { i18n, resolveValidationMessage } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

export interface ProviderCheckState {
  checking: boolean;
  ok: boolean | null;
  message: string;
  failure?: unknown;
}

export function useProviderCatalog() {
  const message = useMessage();
  const backupsStore = useBackupsStore();

  const dialogOpen = ref(false);
  const editingId = ref<string | null>(null);
  const name = ref("");
  const kind = ref<StorageProviderKind>("s3");
  const endpoint = ref("");
  const region = ref("");
  const bucket = ref("");
  const prefix = ref("");
  const accessKey = ref("");
  const secretKey = ref("");
  const checks = ref<Record<string, ProviderCheckState>>({});

  function resetForm(): void {
    editingId.value = null;
    name.value = "";
    kind.value = "s3";
    endpoint.value = "";
    region.value = "";
    bucket.value = "";
    prefix.value = "";
    accessKey.value = "";
    secretKey.value = "";
  }

  function openCreate(): void {
    resetForm();
    dialogOpen.value = true;
  }

  function openEdit(providerId: string): void {
    const provider = backupsStore.providerOf(providerId);
    if (!provider) {
      return;
    }
    editingId.value = provider.id;
    name.value = provider.name;
    kind.value = provider.kind;
    endpoint.value = provider.endpoint ?? "";
    region.value = provider.region ?? "";
    bucket.value = provider.bucket ?? "";
    prefix.value = provider.prefix ?? "";
    accessKey.value = "";
    secretKey.value = "";
    delete checks.value[providerId];
    dialogOpen.value = true;
  }

  function close(): void {
    dialogOpen.value = false;
    resetForm();
  }

  async function save(): Promise<void> {
    const providerError = validateProviderForm({
      name: name.value,
      kind: kind.value,
      endpoint: endpoint.value,
      bucket: bucket.value,
      accessKey: accessKey.value,
      secretKey: secretKey.value,
      isNew: editingId.value === null,
    });
    if (providerError !== null) {
      message.error(resolveValidationMessage(providerError));
      return;
    }
    const trimmed = name.value.trim();
    try {
      if (editingId.value === null) {
        await backupsStore.addProvider({
          name: trimmed,
          kind: kind.value,
          endpoint: endpoint.value,
          region: region.value,
          bucket: bucket.value,
          prefix: prefix.value,
          access_key: accessKey.value,
          secret_key: secretKey.value,
        });
        message.success(t("databases.backups.targets.saved", { name: trimmed }));
      } else {
        await backupsStore.editProvider(editingId.value, {
          name: trimmed,
          kind: kind.value,
          endpoint: endpoint.value,
          region: region.value,
          bucket: bucket.value,
          prefix: prefix.value,
          access_key: accessKey.value,
          secret_key: secretKey.value,
        });
        delete checks.value[editingId.value];
        message.success(
          t("databases.backups.targets.updated", { name: trimmed }),
        );
      }
      close();
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  async function remove(providerId: string, providerName: string): Promise<void> {
    try {
      await backupsStore.removeProvider(providerId);
      delete checks.value[providerId];
      message.success(
        t("databases.backups.targets.deleted", { name: providerName }),
      );
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  async function test(providerId: string): Promise<void> {
    checks.value[providerId] = { checking: true, ok: null, message: "" };
    try {
      const check = await backupsStore.checkProvider(providerId);
      checks.value[providerId] = {
        checking: false,
        ok: check.ok,
        message: check.message,
      };
    } catch (error) {
      checks.value[providerId] = {
        checking: false,
        ok: false,
        message: "",
        failure: error instanceof Error ? (error.cause ?? error) : error,
      };
    }
  }

  function checkMessage(providerId: string): string {
    const state = checks.value[providerId];
    if (!state || state.checking) {
      return "";
    }
    if (state.failure !== undefined) {
      return describeBackupError(state.failure);
    }
    return state.message !== ""
      ? state.message
      : t("databases.backups.targets.testFailed");
  }

  return {
    backupsStore,
    dialogOpen,
    editingId,
    name,
    kind,
    endpoint,
    region,
    bucket,
    prefix,
    accessKey,
    secretKey,
    checks,
    resetForm,
    openCreate,
    openEdit,
    close,
    save,
    remove,
    test,
    checkMessage,
  };
}

export type ProviderCatalog = ReturnType<typeof useProviderCatalog>;

export const providerCatalogKey: InjectionKey<ProviderCatalog> =
  Symbol("provider-catalog");
export { backupMessages };
