import { defineStore } from "pinia";
import { ref } from "vue";

import {
  createBackup,
  createSchedule,
  createTarget,
  deleteBackup,
  deleteSchedule,
  deleteTarget,
  describeBackupError,
  listBackups,
  listRestores,
  listSchedules,
  listTargets,
  restoreBackup,
  testTarget,
  updateSchedule,
  updateTarget,
} from "../api/backups";
import type {
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
} from "../api/backups";
import { mergeBackupsById } from "../utils/storeMerge";

export const useBackupsStore = defineStore("backups", () => {
  const backupsById = ref<Record<string, DatabaseBackup[]>>({});
  const backupsLoading = ref(false);
  const backupsError = ref<string | null>(null);
  const backupsActing = ref(false);

  const restoresById = ref<Record<string, DatabaseRestore[]>>({});
  const restoresLoading = ref(false);
  const restoresError = ref<string | null>(null);

  const schedulesById = ref<Record<string, BackupSchedule[]>>({});
  const schedulesLoading = ref(false);
  const schedulesError = ref<string | null>(null);
  const schedulesActing = ref(false);

  const targets = ref<BackupTarget[]>([]);
  const targetsLoading = ref(false);
  const targetsError = ref<string | null>(null);
  const targetsActing = ref(false);

  /** backupsOf returns the cached runs of one database, if any. */
  function backupsOf(databaseId: string): DatabaseBackup[] {
    return backupsById.value[databaseId] ?? [];
  }

  /** restoresOf returns the cached restore runs of one database, if any. */
  function restoresOf(databaseId: string): DatabaseRestore[] {
    return restoresById.value[databaseId] ?? [];
  }

  /** schedulesOf returns the cached cron entries of one database, if any. */
  function schedulesOf(databaseId: string): BackupSchedule[] {
    return schedulesById.value[databaseId] ?? [];
  }

  /** targetOf resolves a target by id for the schedule labels. */
  function targetOf(targetId: string | undefined): BackupTarget | null {
    if (!targetId) {
      return null;
    }
    return targets.value.find((item) => item.id === targetId) ?? null;
  }

  /** fetchBackups loads the runs of one database, toggling the loading flag. */
  async function fetchBackups(databaseId: string): Promise<void> {
    backupsLoading.value = true;
    backupsError.value = null;
    try {
      const server = await listBackups(databaseId);
      backupsById.value[databaseId] = mergeBackupsById(
        backupsById.value[databaseId] ?? [],
        server,
      );
    } catch (err) {
      backupsError.value = describeBackupError(err);
      throw err;
    } finally {
      backupsLoading.value = false;
    }
  }

  /** refreshBackups reloads the runs without toggling the loading flag. */
  async function refreshBackups(databaseId: string): Promise<void> {
    try {
      const server = await listBackups(databaseId);
      backupsById.value[databaseId] = mergeBackupsById(
        backupsById.value[databaseId] ?? [],
        server,
      );
      backupsError.value = null;
    } catch (err) {
      backupsError.value = describeBackupError(err);
    }
  }

  /** fetchRestores loads the durable restore runs of one database. */
  async function fetchRestores(databaseId: string): Promise<void> {
    restoresLoading.value = true;
    restoresError.value = null;
    try {
      restoresById.value[databaseId] = await listRestores(databaseId);
    } catch (err) {
      restoresError.value = describeBackupError(err);
      throw err;
    } finally {
      restoresLoading.value = false;
    }
  }

  /** refreshRestores reloads the restore runs without toggling the flag. */
  async function refreshRestores(databaseId: string): Promise<void> {
    try {
      restoresById.value[databaseId] = await listRestores(databaseId);
      restoresError.value = null;
    } catch (err) {
      restoresError.value = describeBackupError(err);
    }
  }

  /**
   * backupNow queues a manual backup and prepends the recorded `running` row,
   * so the list shows the pending state immediately.
   */
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
      backupsError.value = describeBackupError(err);
      throw err;
    } finally {
      backupsActing.value = false;
    }
  }

  /** removeBackup deletes one run (artifact first, then the row). */
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
      backupsError.value = describeBackupError(err);
      throw err;
    } finally {
      backupsActing.value = false;
    }
  }

  /** restoreBackup queues a restore of one completed backup. */
  async function restore(
    databaseId: string,
    backupId: string,
  ): Promise<RestoreResult> {
    backupsActing.value = true;
    try {
      return await restoreBackup(databaseId, backupId);
    } catch (err) {
      backupsError.value = describeBackupError(err);
      throw err;
    } finally {
      backupsActing.value = false;
    }
  }

  /** fetchSchedules loads the cron entries of one database. */
  async function fetchSchedules(databaseId: string): Promise<void> {
    schedulesLoading.value = true;
    schedulesError.value = null;
    try {
      schedulesById.value[databaseId] = await listSchedules(databaseId);
    } catch (err) {
      schedulesError.value = describeBackupError(err);
      throw err;
    } finally {
      schedulesLoading.value = false;
    }
  }

  /** applySchedule merges one schedule into the cached list in place. */
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

  /** addSchedule stores one cron entry and merges the row. */
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
      schedulesError.value = describeBackupError(err);
      throw err;
    } finally {
      schedulesActing.value = false;
    }
  }

  /** editSchedule changes one cron entry and merges the row. */
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
      schedulesError.value = describeBackupError(err);
      throw err;
    } finally {
      schedulesActing.value = false;
    }
  }

  /** removeSchedule deletes one cron entry and drops it from the cache. */
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
      schedulesError.value = describeBackupError(err);
      throw err;
    } finally {
      schedulesActing.value = false;
    }
  }

  /** fetchTargets loads the caller's storage targets. */
  async function fetchTargets(): Promise<void> {
    targetsLoading.value = true;
    targetsError.value = null;
    try {
      targets.value = await listTargets();
    } catch (err) {
      targetsError.value = describeBackupError(err);
      throw err;
    } finally {
      targetsLoading.value = false;
    }
  }

  /** applyTarget merges one target into the cached list in place. */
  function applyTarget(updated: BackupTarget): void {
    const index = targets.value.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      targets.value = [updated, ...targets.value];
      return;
    }
    targets.value[index] = updated;
  }

  /** addTarget stores one target and merges the row. */
  async function addTarget(
    input: CreateBackupTargetInput,
  ): Promise<BackupTarget> {
    targetsActing.value = true;
    try {
      const target = await createTarget(input);
      applyTarget(target);
      return target;
    } catch (err) {
      targetsError.value = describeBackupError(err);
      throw err;
    } finally {
      targetsActing.value = false;
    }
  }

  /** editTarget changes one target and merges the row. */
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
      targetsError.value = describeBackupError(err);
      throw err;
    } finally {
      targetsActing.value = false;
    }
  }

  /** removeTarget deletes one target and drops it from the cache. */
  async function removeTarget(targetId: string): Promise<void> {
    targetsActing.value = true;
    try {
      await deleteTarget(targetId);
      targets.value = targets.value.filter((item) => item.id !== targetId);
    } catch (err) {
      targetsError.value = describeBackupError(err);
      throw err;
    } finally {
      targetsActing.value = false;
    }
  }

  /** checkTarget tests one target's connection without touching the cache. */
  async function checkTarget(targetId: string): Promise<TargetCheck> {
    try {
      return await testTarget(targetId);
    } catch (err) {
      throw new Error(describeBackupError(err), { cause: err });
    }
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
    targets,
    targetsLoading,
    targetsError,
    targetsActing,
    backupsOf,
    restoresOf,
    schedulesOf,
    targetOf,
    fetchBackups,
    refreshBackups,
    fetchRestores,
    refreshRestores,
    backupNow,
    removeBackup,
    restore,
    fetchSchedules,
    addSchedule,
    editSchedule,
    removeSchedule,
    fetchTargets,
    addTarget,
    editTarget,
    removeTarget,
    checkTarget,
  };
});
