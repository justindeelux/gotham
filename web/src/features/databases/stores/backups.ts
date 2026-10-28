import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  createBackup,
  createSchedule,
  createTarget,
  deleteBackup,
  deleteSchedule,
  deleteTarget,
  describeBackupError,
  getBackupProtection,
  listBackups,
  listRestores,
  listSchedules,
  listTargets,
  restoreBackup,
  testTarget,
  updateSchedule,
  updateTarget,
} from "@/features/databases/api/backups";
import type {
  BackupProtectionSummary,
  BackupSchedule,
  BackupTarget,
  CreateBackupScheduleInput,
  CreateBackupTargetInput,
  DatabaseBackup,
  DatabaseRestore,
  RestoreResult,
  TargetCheck,
  UpdateBackupScheduleInput,
  UpdateBackupTargetInput,
} from "@/features/databases/api/backups";
import { mergeBackupsById } from "@/features/databases/utils/storeMerge";

export const useBackupsStore = defineStore("backups", () => {
  const backupsById = ref<Record<string, DatabaseBackup[]>>({});
  const backupsLoading = ref(false);
  const backupsActing = ref(false);

  const restoresById = ref<Record<string, DatabaseRestore[]>>({});
  const restoresLoading = ref(false);

  const schedulesById = ref<Record<string, BackupSchedule[]>>({});
  const schedulesLoading = ref(false);
  const schedulesActing = ref(false);

  const protectionById = ref<Record<string, BackupProtectionSummary>>({});
  const protectionLoading = ref(false);
  const protectionErrorRaw = ref<unknown>(null);

  const targets = ref<BackupTarget[]>([]);
  const targetsLoading = ref(false);
  const targetsActing = ref(false);

  const backupsErrorRaw = ref<unknown>(null);
  const restoresErrorRaw = ref<unknown>(null);
  const schedulesErrorRaw = ref<unknown>(null);
  const targetsErrorRaw = ref<unknown>(null);
  const backupsError = computed<string | null>(() =>
    backupsErrorRaw.value === null
      ? null
      : describeBackupError(backupsErrorRaw.value),
  );
  const restoresError = computed<string | null>(() =>
    restoresErrorRaw.value === null
      ? null
      : describeBackupError(restoresErrorRaw.value),
  );
  const schedulesError = computed<string | null>(() =>
    schedulesErrorRaw.value === null
      ? null
      : describeBackupError(schedulesErrorRaw.value),
  );
  const protectionError = computed<string | null>(() =>
    protectionErrorRaw.value === null
      ? null
      : describeBackupError(protectionErrorRaw.value),
  );
  const targetsError = computed<string | null>(() =>
    targetsErrorRaw.value === null
      ? null
      : describeBackupError(targetsErrorRaw.value),
  );

  function backupsOf(databaseId: string): DatabaseBackup[] {
    return backupsById.value[databaseId] ?? [];
  }

  function restoresOf(databaseId: string): DatabaseRestore[] {
    return restoresById.value[databaseId] ?? [];
  }

  function schedulesOf(databaseId: string): BackupSchedule[] {
    return schedulesById.value[databaseId] ?? [];
  }

  function protectionOf(databaseId: string): BackupProtectionSummary | null {
    return protectionById.value[databaseId] ?? null;
  }

  function targetOf(targetId: string | undefined): BackupTarget | null {
    if (!targetId) {
      return null;
    }
    return targets.value.find((item) => item.id === targetId) ?? null;
  }

  async function fetchBackups(databaseId: string): Promise<void> {
    backupsLoading.value = true;
    backupsErrorRaw.value = null;
    try {
      const server = await listBackups(databaseId);
      backupsById.value[databaseId] = mergeBackupsById(
        backupsById.value[databaseId] ?? [],
        server,
      );
    } catch (err) {
      backupsErrorRaw.value = err;
      throw err;
    } finally {
      backupsLoading.value = false;
    }
  }

  async function refreshBackups(databaseId: string): Promise<void> {
    try {
      const server = await listBackups(databaseId);
      backupsById.value[databaseId] = mergeBackupsById(
        backupsById.value[databaseId] ?? [],
        server,
      );
      backupsErrorRaw.value = null;
    } catch (err) {
      backupsErrorRaw.value = err;
    }
  }

  async function fetchRestores(databaseId: string): Promise<void> {
    restoresLoading.value = true;
    restoresErrorRaw.value = null;
    try {
      restoresById.value[databaseId] = await listRestores(databaseId);
    } catch (err) {
      restoresErrorRaw.value = err;
      throw err;
    } finally {
      restoresLoading.value = false;
    }
  }

  async function refreshRestores(databaseId: string): Promise<void> {
    try {
      restoresById.value[databaseId] = await listRestores(databaseId);
      restoresErrorRaw.value = null;
    } catch (err) {
      restoresErrorRaw.value = err;
    }
  }

  async function backupNow(
    databaseId: string,
    targetId?: string,
  ): Promise<DatabaseBackup> {
    backupsActing.value = true;
    try {
      const backup = await createBackup(databaseId, targetId);
      backupsById.value[databaseId] = [
        backup,
        ...(backupsById.value[databaseId] ?? []),
      ];
      return backup;
    } catch (err) {
      backupsErrorRaw.value = err;
      throw err;
    } finally {
      backupsActing.value = false;
    }
  }

  async function removeBackup(
    databaseId: string,
    backupId: string,
  ): Promise<void> {
    backupsActing.value = true;
    try {
      await deleteBackup(databaseId, backupId);
      backupsById.value[databaseId] = (backupsById.value[databaseId] ?? []).filter(
        (item) => item.id !== backupId,
      );
    } catch (err) {
      backupsErrorRaw.value = err;
      throw err;
    } finally {
      backupsActing.value = false;
    }
  }

  async function restore(
    databaseId: string,
    backupId: string,
  ): Promise<RestoreResult> {
    backupsActing.value = true;
    try {
      return await restoreBackup(databaseId, backupId);
    } catch (err) {
      backupsErrorRaw.value = err;
      throw err;
    } finally {
      backupsActing.value = false;
    }
  }

  async function fetchSchedules(databaseId: string): Promise<void> {
    schedulesLoading.value = true;
    schedulesErrorRaw.value = null;
    try {
      schedulesById.value[databaseId] = await listSchedules(databaseId);
    } catch (err) {
      schedulesErrorRaw.value = err;
      throw err;
    } finally {
      schedulesLoading.value = false;
    }
  }

  function applySchedule(databaseId: string, updated: BackupSchedule): void {
    const list = schedulesById.value[databaseId] ?? [];
    const index = list.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      schedulesById.value[databaseId] = [updated, ...list];
      return;
    }
    list[index] = updated;
    schedulesById.value[databaseId] = [...list];
  }

  async function fetchProtection(databaseId: string): Promise<void> {
    protectionLoading.value = true;
    protectionErrorRaw.value = null;
    try {
      protectionById.value[databaseId] = await getBackupProtection(databaseId);
    } catch (err) {
      protectionErrorRaw.value = err;
      throw err;
    } finally {
      protectionLoading.value = false;
    }
  }

  async function addSchedule(
    databaseId: string,
    input: CreateBackupScheduleInput,
  ): Promise<BackupSchedule> {
    schedulesActing.value = true;
    try {
      const schedule = await createSchedule(databaseId, input);
      applySchedule(databaseId, schedule);
      return schedule;
    } catch (err) {
      schedulesErrorRaw.value = err;
      throw err;
    } finally {
      schedulesActing.value = false;
    }
  }

  async function editSchedule(
    databaseId: string,
    scheduleId: string,
    input: UpdateBackupScheduleInput,
  ): Promise<BackupSchedule> {
    schedulesActing.value = true;
    try {
      const schedule = await updateSchedule(databaseId, scheduleId, input);
      applySchedule(databaseId, schedule);
      return schedule;
    } catch (err) {
      schedulesErrorRaw.value = err;
      throw err;
    } finally {
      schedulesActing.value = false;
    }
  }

  async function removeSchedule(
    databaseId: string,
    scheduleId: string,
  ): Promise<void> {
    schedulesActing.value = true;
    try {
      await deleteSchedule(databaseId, scheduleId);
      schedulesById.value[databaseId] = (
        schedulesById.value[databaseId] ?? []
      ).filter((item) => item.id !== scheduleId);
    } catch (err) {
      schedulesErrorRaw.value = err;
      throw err;
    } finally {
      schedulesActing.value = false;
    }
  }

  async function fetchTargets(): Promise<void> {
    targetsLoading.value = true;
    targetsErrorRaw.value = null;
    try {
      targets.value = await listTargets();
    } catch (err) {
      targetsErrorRaw.value = err;
      throw err;
    } finally {
      targetsLoading.value = false;
    }
  }

  function applyTarget(updated: BackupTarget): void {
    const index = targets.value.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      targets.value = [updated, ...targets.value];
      return;
    }
    targets.value[index] = updated;
  }

  async function addTarget(
    input: CreateBackupTargetInput,
  ): Promise<BackupTarget> {
    targetsActing.value = true;
    try {
      const target = await createTarget(input);
      applyTarget(target);
      return target;
    } catch (err) {
      targetsErrorRaw.value = err;
      throw err;
    } finally {
      targetsActing.value = false;
    }
  }

  async function editTarget(
    targetId: string,
    input: UpdateBackupTargetInput,
  ): Promise<BackupTarget> {
    targetsActing.value = true;
    try {
      const target = await updateTarget(targetId, input);
      applyTarget(target);
      return target;
    } catch (err) {
      targetsErrorRaw.value = err;
      throw err;
    } finally {
      targetsActing.value = false;
    }
  }

  async function removeTarget(targetId: string): Promise<void> {
    targetsActing.value = true;
    try {
      await deleteTarget(targetId);
      targets.value = targets.value.filter((item) => item.id !== targetId);
    } catch (err) {
      targetsErrorRaw.value = err;
      throw err;
    } finally {
      targetsActing.value = false;
    }
  }

  async function checkTarget(targetId: string): Promise<TargetCheck> {
    try {
      return await testTarget(targetId);
    } catch (err) {
      throw new Error(describeBackupError(err), { cause: err });
    }
  }

  function reset(): void {
    backupsById.value = {};
    backupsLoading.value = false;
    backupsErrorRaw.value = null;
    backupsActing.value = false;
    restoresById.value = {};
    restoresLoading.value = false;
    restoresErrorRaw.value = null;
    schedulesById.value = {};
    schedulesLoading.value = false;
    schedulesErrorRaw.value = null;
    schedulesActing.value = false;
    protectionById.value = {};
    protectionLoading.value = false;
    protectionErrorRaw.value = null;
    targets.value = [];
    targetsLoading.value = false;
    targetsErrorRaw.value = null;
    targetsActing.value = false;
  }

  return {
    backupsById,
    backupsLoading,
    backupsError,
    backupsActing,
    restoresById,
    restoresLoading,
    restoresError,
    schedulesById,
    schedulesLoading,
    schedulesError,
    schedulesActing,
    protectionById,
    protectionLoading,
    protectionError,
    targets,
    targetsLoading,
    targetsError,
    targetsActing,
    backupsOf,
    restoresOf,
    schedulesOf,
    protectionOf,
    targetOf,
    fetchBackups,
    refreshBackups,
    fetchRestores,
    refreshRestores,
    backupNow,
    removeBackup,
    restore,
    fetchSchedules,
    fetchProtection,
    addSchedule,
    editSchedule,
    removeSchedule,
    fetchTargets,
    addTarget,
    editTarget,
    removeTarget,
    checkTarget,
    reset,
  };
});
