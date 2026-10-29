import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  createBackup,
  createProvider,
  createSchedule,
  deleteBackup,
  deleteProvider,
  deleteSchedule,
  describeBackupError,
  getResourceBackupProtection,
  listBackups,
  listProviders,
  listRestores,
  listSchedules,
  restoreBackup,
  testProvider,
  updateProvider,
  updateSchedule,
} from "@/features/backups/api/backups";
import type {
  BackupProtectionSummary,
  BackupRestore,
  BackupRun,
  BackupRunStatus,
  BackupSchedule,
  CreateProviderInput,
  CreateScheduleInput,
  ProviderCheck,
  ResourceRef,
  RestoreResult,
  StorageProvider,
  UpdateProviderInput,
  UpdateScheduleInput,
} from "@/features/backups/api/backups";
import { mergeBackupsById } from "@/features/backups/utils/backupMerge";

export function resourceKey(ref: ResourceRef): string {
  return `${ref.resource_type}:${ref.resource_id}`;
}

export const useBackupsStore = defineStore("backups", () => {
  const backupsByResource = ref<Record<string, BackupRun[]>>({});
  const restoresByResource = ref<Record<string, BackupRestore[]>>({});
  const schedulesByResource = ref<Record<string, BackupSchedule[]>>({});
  const protectionByResource = ref<Record<string, BackupProtectionSummary>>({});

  const allBackups = ref<BackupRun[]>([]);
  const allSchedules = ref<BackupSchedule[]>([]);

  const backupsLoading = ref(false);
  const restoresLoading = ref(false);
  const schedulesLoading = ref(false);
  const protectionLoading = ref(false);
  const allLoading = ref(false);
  const backupsActing = ref(false);
  const schedulesActing = ref(false);

  const providers = ref<StorageProvider[]>([]);
  const providersLoading = ref(false);
  const providersActing = ref(false);

  const backupsErrorRaw = ref<unknown>(null);
  const restoresErrorRaw = ref<unknown>(null);
  const schedulesErrorRaw = ref<unknown>(null);
  const protectionErrorRaw = ref<unknown>(null);
  const providersErrorRaw = ref<unknown>(null);

  const backupsError = computed<string | null>(() => toString(backupsErrorRaw.value));
  const restoresError = computed<string | null>(() => toString(restoresErrorRaw.value));
  const schedulesError = computed<string | null>(() => toString(schedulesErrorRaw.value));
  const protectionError = computed<string | null>(() => toString(protectionErrorRaw.value));
  const providersError = computed<string | null>(() => toString(providersErrorRaw.value));

  function toString(error: unknown): string | null {
    return error === null ? null : describeBackupError(error);
  }

  function backupsOf(ref: ResourceRef): BackupRun[] {
    return backupsByResource.value[resourceKey(ref)] ?? [];
  }

  function restoresOf(ref: ResourceRef): BackupRestore[] {
    return restoresByResource.value[resourceKey(ref)] ?? [];
  }

  function schedulesOf(ref: ResourceRef): BackupSchedule[] {
    return schedulesByResource.value[resourceKey(ref)] ?? [];
  }

  function protectionOf(ref: ResourceRef): BackupProtectionSummary | null {
    return protectionByResource.value[resourceKey(ref)] ?? null;
  }

  function providerOf(providerId: string | undefined): StorageProvider | null {
    if (!providerId) {
      return null;
    }
    return providers.value.find((item) => item.id === providerId) ?? null;
  }

  async function fetchBackups(ref: ResourceRef): Promise<void> {
    backupsLoading.value = true;
    backupsErrorRaw.value = null;
    try {
      backupsByResource.value[resourceKey(ref)] = mergeBackupsById(
        backupsByResource.value[resourceKey(ref)] ?? [],
        await listBackups({ resource: ref }),
      );
    } catch (err) {
      backupsErrorRaw.value = err;
      throw err;
    } finally {
      backupsLoading.value = false;
    }
  }

  async function refreshBackups(ref: ResourceRef): Promise<void> {
    try {
      backupsByResource.value[resourceKey(ref)] = mergeBackupsById(
        backupsByResource.value[resourceKey(ref)] ?? [],
        await listBackups({ resource: ref }),
      );
      backupsErrorRaw.value = null;
    } catch (err) {
      backupsErrorRaw.value = err;
    }
  }

  async function fetchAllBackups(status?: BackupRunStatus): Promise<void> {
    allLoading.value = true;
    backupsErrorRaw.value = null;
    try {
      allBackups.value = mergeBackupsById(
        allBackups.value,
        await listBackups({ status, limit: 200 }),
      );
    } catch (err) {
      backupsErrorRaw.value = err;
      throw err;
    } finally {
      allLoading.value = false;
    }
  }

  async function fetchRestores(ref: ResourceRef): Promise<void> {
    restoresLoading.value = true;
    restoresErrorRaw.value = null;
    try {
      restoresByResource.value[resourceKey(ref)] = await listRestores(ref);
    } catch (err) {
      restoresErrorRaw.value = err;
      throw err;
    } finally {
      restoresLoading.value = false;
    }
  }

  async function fetchSchedules(ref: ResourceRef): Promise<void> {
    schedulesLoading.value = true;
    schedulesErrorRaw.value = null;
    try {
      schedulesByResource.value[resourceKey(ref)] = await listSchedules(ref);
    } catch (err) {
      schedulesErrorRaw.value = err;
      throw err;
    } finally {
      schedulesLoading.value = false;
    }
  }

  async function fetchAllSchedules(): Promise<void> {
    schedulesLoading.value = true;
    schedulesErrorRaw.value = null;
    try {
      allSchedules.value = await listSchedules();
    } catch (err) {
      schedulesErrorRaw.value = err;
      throw err;
    } finally {
      schedulesLoading.value = false;
    }
  }

  async function fetchProtection(ref: ResourceRef): Promise<void> {
    protectionLoading.value = true;
    protectionErrorRaw.value = null;
    try {
      protectionByResource.value[resourceKey(ref)] =
        await getResourceBackupProtection(
          `${ref.resource_type}s` as
            | "applications"
            | "databases"
            | "services",
          ref.resource_id,
        );
    } catch (err) {
      protectionErrorRaw.value = err;
      throw err;
    } finally {
      protectionLoading.value = false;
    }
  }

  async function backupNow(
    ref: ResourceRef,
    providerId?: string,
  ): Promise<BackupRun> {
    backupsActing.value = true;
    try {
      const backup = await createBackup(ref, providerId);
      const key = resourceKey(ref);
      backupsByResource.value[key] = [
        backup,
        ...(backupsByResource.value[key] ?? []),
      ];
      allBackups.value = [backup, ...allBackups.value];
      return backup;
    } catch (err) {
      backupsErrorRaw.value = err;
      throw err;
    } finally {
      backupsActing.value = false;
    }
  }

  async function removeBackup(backup: BackupRun): Promise<void> {
    backupsActing.value = true;
    try {
      await deleteBackup(backup.id);
      const key = resourceKey(backup);
      backupsByResource.value[key] = (
        backupsByResource.value[key] ?? []
      ).filter((item) => item.id !== backup.id);
      allBackups.value = allBackups.value.filter((item) => item.id !== backup.id);
    } catch (err) {
      backupsErrorRaw.value = err;
      throw err;
    } finally {
      backupsActing.value = false;
    }
  }

  async function restore(backupId: string): Promise<RestoreResult> {
    backupsActing.value = true;
    try {
      return await restoreBackup(backupId);
    } catch (err) {
      backupsErrorRaw.value = err;
      throw err;
    } finally {
      backupsActing.value = false;
    }
  }

  function applySchedule(ref: ResourceRef, updated: BackupSchedule): void {
    const key = resourceKey(ref);
    const list = schedulesByResource.value[key] ?? [];
    const index = list.findIndex((item) => item.id === updated.id);
    schedulesByResource.value[key] =
      index === -1
        ? [updated, ...list]
        : list.map((item) => (item.id === updated.id ? updated : item));

    const allIndex = allSchedules.value.findIndex(
      (item) => item.id === updated.id,
    );
    allSchedules.value =
      allIndex === -1
        ? [updated, ...allSchedules.value]
        : allSchedules.value.map((item) =>
            item.id === updated.id ? updated : item,
          );
  }

  async function addSchedule(
    ref: ResourceRef,
    input: Omit<CreateScheduleInput, "resource">,
  ): Promise<BackupSchedule> {
    schedulesActing.value = true;
    try {
      const schedule = await createSchedule({ ...input, resource: ref });
      applySchedule(ref, schedule);
      return schedule;
    } catch (err) {
      schedulesErrorRaw.value = err;
      throw err;
    } finally {
      schedulesActing.value = false;
    }
  }

  async function editSchedule(
    ref: ResourceRef,
    scheduleId: string,
    input: UpdateScheduleInput,
  ): Promise<BackupSchedule> {
    schedulesActing.value = true;
    try {
      const schedule = await updateSchedule(scheduleId, input);
      applySchedule(ref, schedule);
      return schedule;
    } catch (err) {
      schedulesErrorRaw.value = err;
      throw err;
    } finally {
      schedulesActing.value = false;
    }
  }

  async function removeSchedule(
    ref: ResourceRef,
    scheduleId: string,
  ): Promise<void> {
    schedulesActing.value = true;
    try {
      await deleteSchedule(scheduleId);
      const key = resourceKey(ref);
      schedulesByResource.value[key] = (
        schedulesByResource.value[key] ?? []
      ).filter((item) => item.id !== scheduleId);
      allSchedules.value = allSchedules.value.filter(
        (item) => item.id !== scheduleId,
      );
    } catch (err) {
      schedulesErrorRaw.value = err;
      throw err;
    } finally {
      schedulesActing.value = false;
    }
  }

  async function fetchProviders(): Promise<void> {
    providersLoading.value = true;
    providersErrorRaw.value = null;
    try {
      providers.value = await listProviders();
    } catch (err) {
      providersErrorRaw.value = err;
      throw err;
    } finally {
      providersLoading.value = false;
    }
  }

  function applyProvider(updated: StorageProvider): void {
    const index = providers.value.findIndex((item) => item.id === updated.id);
    providers.value =
      index === -1
        ? [updated, ...providers.value]
        : providers.value.map((item) => (item.id === updated.id ? updated : item));
  }

  async function addProvider(
    input: CreateProviderInput,
  ): Promise<StorageProvider> {
    providersActing.value = true;
    try {
      const provider = await createProvider(input);
      applyProvider(provider);
      return provider;
    } catch (err) {
      providersErrorRaw.value = err;
      throw err;
    } finally {
      providersActing.value = false;
    }
  }

  async function editProvider(
    providerId: string,
    input: UpdateProviderInput,
  ): Promise<StorageProvider> {
    providersActing.value = true;
    try {
      const provider = await updateProvider(providerId, input);
      applyProvider(provider);
      return provider;
    } catch (err) {
      providersErrorRaw.value = err;
      throw err;
    } finally {
      providersActing.value = false;
    }
  }

  async function removeProvider(providerId: string): Promise<void> {
    providersActing.value = true;
    try {
      await deleteProvider(providerId);
      providers.value = providers.value.filter((item) => item.id !== providerId);
    } catch (err) {
      providersErrorRaw.value = err;
      throw err;
    } finally {
      providersActing.value = false;
    }
  }

  async function checkProvider(providerId: string): Promise<ProviderCheck> {
    try {
      return await testProvider(providerId);
    } catch (err) {
      throw new Error(describeBackupError(err), { cause: err });
    }
  }

  function reset(): void {
    backupsByResource.value = {};
    restoresByResource.value = {};
    schedulesByResource.value = {};
    protectionByResource.value = {};
    allBackups.value = [];
    allSchedules.value = [];
    backupsLoading.value = false;
    restoresLoading.value = false;
    schedulesLoading.value = false;
    protectionLoading.value = false;
    allLoading.value = false;
    backupsActing.value = false;
    schedulesActing.value = false;
    providers.value = [];
    providersLoading.value = false;
    providersActing.value = false;
    backupsErrorRaw.value = null;
    restoresErrorRaw.value = null;
    schedulesErrorRaw.value = null;
    protectionErrorRaw.value = null;
    providersErrorRaw.value = null;
  }

  return {
    backupsByResource,
    restoresByResource,
    schedulesByResource,
    protectionByResource,
    allBackups,
    allSchedules,
    backupsLoading,
    restoresLoading,
    schedulesLoading,
    protectionLoading,
    allLoading,
    backupsActing,
    schedulesActing,
    providers,
    providersLoading,
    providersActing,
    backupsError,
    restoresError,
    schedulesError,
    protectionError,
    providersError,
    backupsOf,
    restoresOf,
    schedulesOf,
    protectionOf,
    providerOf,
    fetchBackups,
    refreshBackups,
    fetchAllBackups,
    fetchRestores,
    fetchSchedules,
    fetchAllSchedules,
    fetchProtection,
    backupNow,
    removeBackup,
    restore,
    addSchedule,
    editSchedule,
    removeSchedule,
    fetchProviders,
    addProvider,
    editProvider,
    removeProvider,
    checkProvider,
    reset,
  };
});
