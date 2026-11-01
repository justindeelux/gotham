import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  createStorage,
  deleteStorage,
  describeStorageError,
  listStorages,
  testStorage,
  updateStorage,
} from "@/features/storages/api/storages";
import type {
  CreateStorageInput,
  UpdateStorageInput,
} from "@/features/storages/api/storages";
import type { Storage, StorageCheck } from "@/features/storages/schemas/storages";

export const useStoragesStore = defineStore("storages", () => {
  const items = ref<Storage[]>([]);
  const loading = ref(false);
  const acting = ref(false);
  const errorRaw = ref<unknown>(null);

  const error = computed<string | null>(() =>
    errorRaw.value === null ? null : describeStorageError(errorRaw.value),
  );

  function storageOf(storageId: string | undefined): Storage | null {
    if (!storageId) {
      return null;
    }
    return items.value.find((item) => item.id === storageId) ?? null;
  }

  function apply(updated: Storage): void {
    const index = items.value.findIndex((item) => item.id === updated.id);
    items.value =
      index === -1
        ? [updated, ...items.value]
        : items.value.map((item) => (item.id === updated.id ? updated : item));
  }

  async function fetch(): Promise<void> {
    loading.value = true;
    errorRaw.value = null;
    try {
      items.value = await listStorages();
    } catch (err) {
      errorRaw.value = err;
      throw err;
    } finally {
      loading.value = false;
    }
  }

  async function create(input: CreateStorageInput): Promise<Storage> {
    acting.value = true;
    try {
      const created = await createStorage(input);
      apply(created);
      return created;
    } catch (err) {
      errorRaw.value = err;
      throw err;
    } finally {
      acting.value = false;
    }
  }

  async function update(
    storageId: string,
    input: UpdateStorageInput,
  ): Promise<Storage> {
    acting.value = true;
    try {
      const updated = await updateStorage(storageId, input);
      apply(updated);
      return updated;
    } catch (err) {
      errorRaw.value = err;
      throw err;
    } finally {
      acting.value = false;
    }
  }

  async function remove(storageId: string, confirm = false): Promise<void> {
    acting.value = true;
    try {
      await deleteStorage(storageId, confirm);
      items.value = items.value.filter((item) => item.id !== storageId);
    } catch (err) {
      errorRaw.value = err;
      throw err;
    } finally {
      acting.value = false;
    }
  }

  async function test(storageId: string): Promise<StorageCheck> {
    try {
      return await testStorage(storageId);
    } catch (err) {
      throw new Error(describeStorageError(err), { cause: err });
    }
  }

  function reset(): void {
    items.value = [];
    loading.value = false;
    acting.value = false;
    errorRaw.value = null;
  }

  return {
    items,
    loading,
    acting,
    error,
    storageOf,
    fetch,
    create,
    update,
    remove,
    test,
    reset,
  };
});
