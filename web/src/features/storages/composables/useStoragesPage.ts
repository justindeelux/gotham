import { useMessage } from "naive-ui";
import { computed, ref } from "vue";

import { describeStorageError } from "@/features/storages/api/storages";
import type { StorageKind } from "@/features/storages/schemas/storages";
import { firstStorageFormIssue } from "@/features/storages/schemas/storages";
import { useStoragesStore } from "@/features/storages/stores/storages";
import { i18n, resolveValidationMessage } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

export interface StorageCheckState {
  checking: boolean;
  ok: boolean | null;
  message: string;
  failure?: unknown;
}

export function useStoragesPage() {
  const message = useMessage();
  const store = useStoragesStore();

  const dialogOpen = ref(false);
  const editingId = ref<string | null>(null);
  const name = ref("");
  const kind = ref<StorageKind>("s3");
  const endpoint = ref("");
  const region = ref("");
  const bucket = ref("");
  const prefix = ref("");
  const accessKey = ref("");
  const secretKey = ref("");
  const checks = ref<Record<string, StorageCheckState>>({});

  const onlyBuiltin = computed(
    () => store.items.length > 0 && store.items.every((item) => item.builtin),
  );

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

  function openEdit(storageId: string): void {
    const storage = store.storageOf(storageId);
    if (!storage) {
      return;
    }
    editingId.value = storage.id;
    name.value = storage.name;
    kind.value = storage.kind;
    endpoint.value = storage.endpoint ?? "";
    region.value = storage.region ?? "";
    bucket.value = storage.bucket ?? "";
    prefix.value = storage.prefix ?? "";
    accessKey.value = "";
    secretKey.value = "";
    delete checks.value[storageId];
    dialogOpen.value = true;
  }

  function close(): void {
    dialogOpen.value = false;
    resetForm();
  }

  async function load(): Promise<void> {
    try {
      await store.fetch();
    } catch {
      // The panel renders store.error; a toast on load would duplicate it.
    }
  }

  async function save(): Promise<void> {
    const issue = firstStorageFormIssue({
      name: name.value,
      kind: kind.value,
      endpoint: endpoint.value,
      bucket: bucket.value,
      accessKey: accessKey.value,
      secretKey: secretKey.value,
      isNew: editingId.value === null,
    });
    if (issue !== null) {
      message.error(resolveValidationMessage(issue));
      return;
    }
    const trimmed = name.value.trim();
    const input = {
      name: trimmed,
      kind: kind.value,
      endpoint: endpoint.value,
      region: region.value,
      bucket: bucket.value,
      prefix: prefix.value,
      access_key: accessKey.value,
      secret_key: secretKey.value,
    };
    try {
      if (editingId.value === null) {
        await store.create(input);
        message.success(t("storages.saved", { name: trimmed }));
      } else {
        const editedId = editingId.value;
        await store.update(editedId, input);
        delete checks.value[editedId];
        message.success(t("storages.updated", { name: trimmed }));
      }
      close();
    } catch (error) {
      message.error(describeStorageError(error));
    }
  }

  async function remove(storageId: string, storageName: string): Promise<void> {
    try {
      // The popconfirm the owner accepts is the confirmation for past backups
      // becoming unreadable; a storage still used by a schedule is rejected
      // by the API regardless.
      await store.remove(storageId, true);
      delete checks.value[storageId];
      message.success(t("storages.deleted", { name: storageName }));
    } catch (error) {
      message.error(describeStorageError(error));
    }
  }

  async function test(storageId: string): Promise<void> {
    checks.value[storageId] = { checking: true, ok: null, message: "" };
    try {
      const check = await store.test(storageId);
      checks.value[storageId] = {
        checking: false,
        ok: check.ok,
        message: check.message,
      };
    } catch (error) {
      checks.value[storageId] = {
        checking: false,
        ok: false,
        message: "",
        failure: error instanceof Error ? (error.cause ?? error) : error,
      };
    }
  }

  function checkMessage(storageId: string): string {
    const state = checks.value[storageId];
    if (!state || state.checking) {
      return "";
    }
    if (state.failure !== undefined) {
      return describeStorageError(state.failure);
    }
    return state.message !== "" ? state.message : t("storages.testFailed");
  }

  return {
    store,
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
    onlyBuiltin,
    resetForm,
    openCreate,
    openEdit,
    close,
    load,
    save,
    remove,
    test,
    checkMessage,
  };
}

export type StoragesPageState = ReturnType<typeof useStoragesPage>;
