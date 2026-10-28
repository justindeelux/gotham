import { useMessage } from "naive-ui";
import type { SelectOption } from "naive-ui";
import { computed, onUnmounted, ref, watch } from "vue";
import type { InjectionKey, Ref } from "vue";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

import { describeBackupError } from "@/features/databases/api/backups";
import type {
  BackupSchedule,
  BackupTargetKind,
  DatabaseBackup,
} from "@/features/databases/api/backups";
import {
  databaseMessages,
  isCronPresent,
  validateTargetForm,
} from "@/features/databases/schemas/databases";
import { useBackupsStore } from "@/features/databases/stores/backups";
import { advanceRestoreStatuses } from "@/features/databases/utils/restoreOutcomes";
import { i18n, resolveValidationMessage } from "@/shared/i18n";

export interface TargetTestState {
  checking: boolean;
  ok: boolean | null;
  message: string;
  failure?: unknown;
}

export const CRON_PRESETS = ["0 2 * * *", "0 */6 * * *", "0 3 * * 0"];

export function useDatabaseBackups(
  dbId: Ref<string>,
  activeTab: Ref<string>,
) {
  const message = useMessage();
  const backupsStore = useBackupsStore();

  const backupTargetId = ref("");
  const restoreOpen = ref(false);
  const restoreCandidate = ref<DatabaseBackup | null>(null);
  const restoring = ref(false);

  const scheduleCron = ref("");
  const scheduleTargetId = ref("");
  const scheduleEnabled = ref(true);
  const scheduleMaxRunMinutes = ref(120);
  const editingScheduleId = ref<string | null>(null);

  const targetEditingId = ref<string | null>(null);
  const targetName = ref("");
  const targetKind = ref<BackupTargetKind>("s3");
  const targetEndpoint = ref("");
  const targetRegion = ref("");
  const targetBucket = ref("");
  const targetPrefix = ref("");
  const targetAccessKey = ref("");
  const targetSecretKey = ref("");

  const targetTests = ref<Record<string, TargetTestState>>({});

  let backupPollTimer: ReturnType<typeof setInterval> | null = null;

  const backups = computed(() => {
    const list = backupsStore.backupsOf(dbId.value);
    return [...list].sort((a, b) => b.created_at.localeCompare(a.created_at));
  });

  const hasRunningBackup = computed<boolean>(() =>
    backups.value.some((item) => item.status === "running"),
  );

  const restores = computed(() => {
    const list = backupsStore.restoresOf(dbId.value);
    return [...list].sort((a, b) => b.created_at.localeCompare(a.created_at));
  });

  const pendingRestoreIds = ref<Set<string>>(new Set());

  const hasRunningRestore = computed<boolean>(
    () =>
      pendingRestoreIds.value.size > 0 ||
      restores.value.some((item) => item.status === "running"),
  );

  const lastRestoreStatus = new Map<string, string>();

  let restorePollEpoch = 0;

  const backupTargetOptions = computed<SelectOption[]>(() => {
    const options: SelectOption[] = [
      { label: t("databases.backups.targets.localDefault"), value: "" },
    ];
    for (const item of backupsStore.targets) {
      options.push({
        label: `${item.name} · ${item.bucket || item.kind}`,
        value: item.id,
      });
    }
    return options;
  });

  const targetKindOptions = computed<SelectOption[]>(() => [
    { label: t("databases.backups.targets.kindS3"), value: "s3" },
    { label: t("databases.backups.targets.kindLocal"), value: "local" },
  ]);

  const scheduleTargetOptions = computed<SelectOption[]>(
    () => backupTargetOptions.value,
  );

  function targetLabel(targetId: string | undefined): string {
    if (!targetId) {
      return t("databases.backups.targets.localDisk");
    }
    return (
      backupsStore.targetOf(targetId)?.name ??
      t("databases.backups.targets.deletedTarget")
    );
  }

  async function fetchBackupTab(): Promise<void> {
    if (!dbId.value) {
      return;
    }
    await Promise.allSettled([
      backupsStore.fetchBackups(dbId.value),
      backupsStore.fetchRestores(dbId.value),
      backupsStore.fetchSchedules(dbId.value),
      backupsStore.fetchProtection(dbId.value),
      backupsStore.fetchTargets(),
    ]);
    notifyRestoreOutcomes();
  }

  function stopBackupPolling(): void {
    if (backupPollTimer !== null) {
      clearInterval(backupPollTimer);
      backupPollTimer = null;
    }
  }

  function reconcilePendingRestores(): void {
    if (pendingRestoreIds.value.size === 0) {
      return;
    }
    const seen = new Set(restores.value.map((item) => item.id));
    const next = new Set<string>();
    for (const id of pendingRestoreIds.value) {
      if (!seen.has(id)) {
        next.add(id);
      }
    }
    if (next.size !== pendingRestoreIds.value.size) {
      pendingRestoreIds.value = next;
    }
  }

  function notifyRestoreOutcomes(): void {
    reconcilePendingRestores();
    for (const outcome of advanceRestoreStatuses(
      lastRestoreStatus,
      restores.value,
    )) {
      if (outcome.status === "completed") {
        message.success(t("databases.backups.restores.completed"));
      } else if (outcome.error) {
        message.error(
          t("databases.backups.restores.failedWithDetail", {
            detail: outcome.error,
          }),
        );
      } else {
        message.error(t("databases.backups.restores.failed"));
      }
    }
  }

  async function pollRunningJobs(id: string): Promise<void> {
    const epoch = ++restorePollEpoch;
    await Promise.allSettled([
      backupsStore.refreshBackups(id),
      backupsStore.refreshRestores(id),
      backupsStore.fetchProtection(id),
    ]);
    if (epoch !== restorePollEpoch) {
      return;
    }
    notifyRestoreOutcomes();
  }

  function syncBackupPolling(): void {
    stopBackupPolling();
    if (
      activeTab.value === "backups" &&
      (hasRunningBackup.value || hasRunningRestore.value) &&
      dbId.value
    ) {
      const id = dbId.value;
      backupPollTimer = setInterval(() => {
        void pollRunningJobs(id);
      }, 5_000);
    }
  }

  async function handleBackupNow(): Promise<void> {
    try {
      await backupsStore.backupNow(dbId.value, backupTargetId.value);
      message.success(t("databases.backups.runs.queued"));
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  async function handleDeleteBackup(backupId: string): Promise<void> {
    try {
      await backupsStore.removeBackup(dbId.value, backupId);
      message.success(t("databases.backups.runs.deleted"));
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  function openRestore(backup: DatabaseBackup): void {
    restoreCandidate.value = backup;
    restoreOpen.value = true;
  }

  async function handleRestoreConfirm(): Promise<void> {
    if (!restoreCandidate.value) {
      return;
    }
    restoring.value = true;
    try {
      const queued = await backupsStore.restore(
        dbId.value,
        restoreCandidate.value.id,
      );
      message.success(t("databases.backups.restores.queued"));
      restoreOpen.value = false;
      restoreCandidate.value = null;
      if (queued.restore_id) {
        lastRestoreStatus.set(queued.restore_id, queued.status);
        pendingRestoreIds.value = new Set(pendingRestoreIds.value).add(
          queued.restore_id,
        );
      }
      await backupsStore.refreshRestores(dbId.value);
      notifyRestoreOutcomes();
    } catch (error) {
      message.error(describeBackupError(error));
    } finally {
      syncBackupPolling();
      restoring.value = false;
    }
  }

  function resetScheduleForm(): void {
    editingScheduleId.value = null;
    scheduleCron.value = "";
    scheduleTargetId.value = "";
    scheduleEnabled.value = true;
    scheduleMaxRunMinutes.value = 120;
  }

  function openScheduleEdit(schedule: BackupSchedule): void {
    editingScheduleId.value = schedule.id;
    scheduleCron.value = schedule.cron;
    scheduleTargetId.value = backupsStore.targetOf(schedule.target_id)
      ? schedule.target_id ?? ""
      : "";
    scheduleEnabled.value = schedule.enabled;
    scheduleMaxRunMinutes.value = Math.round(schedule.max_run_seconds / 60);
  }

  function validMaxRunMinutes(): boolean {
    const minutes = scheduleMaxRunMinutes.value;
    return (
      Number.isFinite(minutes) && minutes >= 15 && minutes <= 24 * 60
    );
  }

  async function handleCreateSchedule(): Promise<void> {
    const cron = scheduleCron.value.trim();
    if (!isCronPresent(scheduleCron.value)) {
      message.error(resolveValidationMessage(databaseMessages.cronRequired));
      return;
    }
    if (!validMaxRunMinutes()) {
      message.error(t("databases.backups.protection.maxRunInvalid"));
      return;
    }
    const maxRunSeconds = Math.round(scheduleMaxRunMinutes.value * 60);
    try {
      if (editingScheduleId.value !== null) {
        await backupsStore.editSchedule(dbId.value, editingScheduleId.value, {
          cron,
          target_id: scheduleTargetId.value,
          enabled: scheduleEnabled.value,
          max_run_seconds: maxRunSeconds,
        });
        message.success(
          t("databases.backups.schedules.updated", { cron }),
        );
      } else {
        await backupsStore.addSchedule(dbId.value, {
          cron,
          target_id: scheduleTargetId.value,
          enabled: scheduleEnabled.value,
          max_run_seconds: maxRunSeconds,
        });
        message.success(t("databases.backups.schedules.saved", { cron }));
      }
      resetScheduleForm();
      await backupsStore.fetchProtection(dbId.value);
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  async function handleToggleSchedule(
    scheduleId: string,
    cron: string,
    enabled: boolean,
  ): Promise<void> {
    try {
      await backupsStore.editSchedule(dbId.value, scheduleId, { cron, enabled });
      message.success(
        enabled
          ? t("databases.backups.schedules.enabled")
          : t("databases.backups.schedules.paused"),
      );
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  async function handleDeleteSchedule(scheduleId: string): Promise<void> {
    try {
      await backupsStore.removeSchedule(dbId.value, scheduleId);
      if (editingScheduleId.value === scheduleId) {
        resetScheduleForm();
      }
      message.success(t("databases.backups.schedules.deleted"));
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  function resetTargetForm(): void {
    targetEditingId.value = null;
    targetName.value = "";
    targetKind.value = "s3";
    targetEndpoint.value = "";
    targetRegion.value = "";
    targetBucket.value = "";
    targetPrefix.value = "";
    targetAccessKey.value = "";
    targetSecretKey.value = "";
  }

  function openTargetEdit(targetId: string): void {
    const target = backupsStore.targets.find((item) => item.id === targetId);
    if (!target) {
      return;
    }
    targetEditingId.value = target.id;
    targetName.value = target.name;
    targetKind.value = target.kind;
    targetEndpoint.value = target.endpoint ?? "";
    targetRegion.value = target.region ?? "";
    targetBucket.value = target.bucket ?? "";
    targetPrefix.value = target.prefix ?? "";
    targetAccessKey.value = "";
    targetSecretKey.value = "";
    delete targetTests.value[targetId];
  }

  async function handleSaveTarget(): Promise<void> {
    const name = targetName.value.trim();
    const targetError = validateTargetForm({
      name: targetName.value,
      kind: targetKind.value,
      endpoint: targetEndpoint.value,
      bucket: targetBucket.value,
      accessKey: targetAccessKey.value,
      secretKey: targetSecretKey.value,
      isNew: targetEditingId.value === null,
    });
    if (targetError !== null) {
      message.error(resolveValidationMessage(targetError));
      return;
    }
    try {
      if (targetEditingId.value === null) {
        await backupsStore.addTarget({
          name,
          kind: targetKind.value,
          endpoint: targetEndpoint.value,
          region: targetRegion.value,
          bucket: targetBucket.value,
          prefix: targetPrefix.value,
          access_key: targetAccessKey.value,
          secret_key: targetSecretKey.value,
        });
        message.success(t("databases.backups.targets.saved", { name }));
      } else {
        await backupsStore.editTarget(targetEditingId.value, {
          name,
          kind: targetKind.value,
          endpoint: targetEndpoint.value,
          region: targetRegion.value,
          bucket: targetBucket.value,
          prefix: targetPrefix.value,
          access_key: targetAccessKey.value,
          secret_key: targetSecretKey.value,
        });
        delete targetTests.value[targetEditingId.value];
        message.success(t("databases.backups.targets.updated", { name }));
      }
      resetTargetForm();
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  async function handleDeleteTarget(targetId: string, name: string): Promise<void> {
    try {
      await backupsStore.removeTarget(targetId);
      if (backupTargetId.value === targetId) {
        backupTargetId.value = "";
      }
      if (scheduleTargetId.value === targetId) {
        scheduleTargetId.value = "";
      }
      delete targetTests.value[targetId];
      message.success(t("databases.backups.targets.deleted", { name }));
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  function targetTestMessage(targetId: string): string {
    const state = targetTests.value[targetId];
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

  async function handleTestTarget(targetId: string): Promise<void> {
    targetTests.value[targetId] = { checking: true, ok: null, message: "" };
    try {
      const check = await backupsStore.checkTarget(targetId);
      targetTests.value[targetId] = {
        checking: false,
        ok: check.ok,
        message: check.message,
      };
    } catch (error) {
      targetTests.value[targetId] = {
        checking: false,
        ok: false,
        message: "",
        failure: error instanceof Error ? (error.cause ?? error) : error,
      };
    }
  }

  function resetView(): void {
    restoreOpen.value = false;
    restoreCandidate.value = null;
    resetTargetForm();
    resetScheduleForm();
    lastRestoreStatus.clear();
    pendingRestoreIds.value = new Set();
    stopBackupPolling();
  }

  watch(dbId, () => {
    resetView();
  });

  watch(activeTab, () => {
    if (activeTab.value === "backups") {
      void fetchBackupTab();
    }
    syncBackupPolling();
  });

  watch(hasRunningBackup, () => {
    syncBackupPolling();
  });

  watch(hasRunningRestore, () => {
    notifyRestoreOutcomes();
    syncBackupPolling();
  });

  onUnmounted(() => {
    stopBackupPolling();
  });

  return {
    backupsStore,
    dbId,
    backupTargetId,
    restoreOpen,
    restoreCandidate,
    restoring,
    scheduleCron,
    scheduleTargetId,
    scheduleEnabled,
    scheduleMaxRunMinutes,
    editingScheduleId,
    targetEditingId,
    targetName,
    targetKind,
    targetEndpoint,
    targetRegion,
    targetBucket,
    targetPrefix,
    targetAccessKey,
    targetSecretKey,
    targetTests,
    backups,
    hasRunningBackup,
    restores,
    hasRunningRestore,
    backupTargetOptions,
    scheduleTargetOptions,
    targetKindOptions,
    targetLabel,
    fetchBackupTab,
    stopBackupPolling,
    syncBackupPolling,
    notifyRestoreOutcomes,
    handleBackupNow,
    handleDeleteBackup,
    openRestore,
    handleRestoreConfirm,
    resetScheduleForm,
    openScheduleEdit,
    handleCreateSchedule,
    handleToggleSchedule,
    handleDeleteSchedule,
    resetTargetForm,
    openTargetEdit,
    handleSaveTarget,
    handleDeleteTarget,
    targetTestMessage,
    handleTestTarget,
    resetView,
  };
}

export type DatabaseBackups = ReturnType<typeof useDatabaseBackups>;

export const databaseBackupsKey: InjectionKey<DatabaseBackups> =
  Symbol("database-backups");
