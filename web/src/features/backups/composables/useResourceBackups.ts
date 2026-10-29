import { useMessage } from "naive-ui";
import type { SelectOption } from "naive-ui";
import { computed, onUnmounted, ref, watch } from "vue";
import type { InjectionKey, Ref } from "vue";

import { describeBackupError } from "@/features/backups/api/backups";
import type {
  BackupRun,
  BackupSchedule,
  ResourceRef,
  ResourceType,
} from "@/features/backups/api/backups";
import { CRON_PRESETS } from "@/features/backups/utils/backupStatus";
import { backupMessages, isCronPresent } from "@/features/backups/schemas/backups";
import { useBackupsStore } from "@/features/backups/stores/backups";
import { advanceRestoreStatuses } from "@/features/backups/utils/restoreOutcomes";
import { useAuthStore } from "@/features/auth";
import { i18n, resolveValidationMessage } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

export const RESOURCE_BACKUP_CRON_PRESETS = CRON_PRESETS;

export function useResourceBackups(
  resourceType: Ref<ResourceType>,
  resourceId: Ref<string>,
  active: Ref<boolean>,
) {
  const message = useMessage();
  const backupsStore = useBackupsStore();
  const authStore = useAuthStore();

  const resource = computed<ResourceRef>(() => ({
    resource_type: resourceType.value,
    resource_id: resourceId.value,
  }));

  const backupProviderId = ref("");
  const restoreOpen = ref(false);
  const restoreCandidate = ref<BackupRun | null>(null);
  const restoring = ref(false);

  const scheduleCron = ref("");
  const scheduleProviderId = ref("");
  const scheduleEnabled = ref(true);
  const scheduleMaxRunMinutes = ref(120);
  const editingScheduleId = ref<string | null>(null);

  let backupPollTimer: ReturnType<typeof setInterval> | null = null;

  const backups = computed(() => {
    const list = backupsStore.backupsOf(resource.value);
    return [...list].sort((a, b) => b.created_at.localeCompare(a.created_at));
  });

  const hasRunningBackup = computed<boolean>(() =>
    backups.value.some((item) => item.status === "running"),
  );

  const restores = computed(() => {
    const list = backupsStore.restoresOf(resource.value);
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

  const backupProviderOptions = computed<SelectOption[]>(() => [
    { label: t("databases.backups.targets.localDefault"), value: "" },
    ...backupsStore.providers
      .filter((item) => item.kind === "s3")
      .map((item) => ({
        label: `${item.name} · ${item.bucket || item.kind}`,
        value: item.id,
      })),
  ]);

  const scheduleProviderOptions = computed<SelectOption[]>(
    () => backupProviderOptions.value,
  );

  function providerLabel(
    providerId: string | undefined,
    runProviderName?: string,
  ): string {
    if (!providerId) {
      return t("databases.backups.targets.localDisk");
    }
    return (
      backupsStore.providerOf(providerId)?.name ??
      runProviderName ??
      t("backups.storage.unknownProvider")
    );
  }

  const protection = computed(() =>
    resourceType.value === "database"
      ? backupsStore.protectionOf(resourceId.value)
      : null,
  );

  const showProtection = computed(() => resourceType.value === "database");

  async function fetchBackupTab(): Promise<void> {
    if (!resourceId.value) {
      return;
    }
    const ref = resource.value;
    await Promise.allSettled([
      backupsStore.fetchBackups(ref),
      backupsStore.fetchRestores(ref),
      backupsStore.fetchSchedules(ref),
      resourceType.value === "database"
        ? backupsStore.fetchProtection(resourceId.value)
        : Promise.resolve(),
      authStore.isOwner ? backupsStore.fetchProviders() : Promise.resolve(),
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

  async function pollRunningJobs(ref: ResourceRef): Promise<void> {
    const epoch = ++restorePollEpoch;
    await Promise.allSettled([
      backupsStore.refreshBackups(ref),
      backupsStore.fetchRestores(ref),
      resourceType.value === "database"
        ? backupsStore.fetchProtection(resourceId.value)
        : Promise.resolve(),
    ]);
    if (epoch !== restorePollEpoch) {
      return;
    }
    notifyRestoreOutcomes();
  }

  function syncBackupPolling(): void {
    stopBackupPolling();
    if (
      active.value &&
      (hasRunningBackup.value || hasRunningRestore.value) &&
      resourceId.value
    ) {
      const ref = resource.value;
      backupPollTimer = setInterval(() => {
        void pollRunningJobs(ref);
      }, 5_000);
    }
  }

  async function handleBackupNow(): Promise<void> {
    try {
      await backupsStore.backupNow(resource.value, backupProviderId.value);
      message.success(t("databases.backups.runs.queued"));
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  async function handleDeleteBackup(backup: BackupRun): Promise<void> {
    try {
      await backupsStore.removeBackup(backup);
      message.success(t("databases.backups.runs.deleted"));
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  function openRestore(backup: BackupRun): void {
    restoreCandidate.value = backup;
    restoreOpen.value = true;
  }

  async function handleRestoreConfirm(): Promise<void> {
    if (!restoreCandidate.value) {
      return;
    }
    restoring.value = true;
    try {
      const queued = await backupsStore.restore(restoreCandidate.value.id);
      message.success(t("databases.backups.restores.queued"));
      restoreOpen.value = false;
      restoreCandidate.value = null;
      if (queued.restore_id) {
        lastRestoreStatus.set(queued.restore_id, queued.status);
        pendingRestoreIds.value = new Set(pendingRestoreIds.value).add(
          queued.restore_id,
        );
      }
      await backupsStore.fetchRestores(resource.value);
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
    scheduleProviderId.value = "";
    scheduleEnabled.value = true;
    scheduleMaxRunMinutes.value = 120;
  }

  function openScheduleEdit(schedule: BackupSchedule): void {
    editingScheduleId.value = schedule.id;
    scheduleCron.value = schedule.cron;
    scheduleProviderId.value = backupsStore.providerOf(schedule.provider_id)
      ? schedule.provider_id ?? ""
      : "";
    scheduleEnabled.value = schedule.enabled;
    scheduleMaxRunMinutes.value = Math.round(schedule.max_run_seconds / 60);
  }

  function validMaxRunMinutes(): boolean {
    const minutes = scheduleMaxRunMinutes.value;
    return Number.isFinite(minutes) && minutes >= 15 && minutes <= 24 * 60;
  }

  async function handleCreateSchedule(): Promise<void> {
    const cron = scheduleCron.value.trim();
    if (!isCronPresent(scheduleCron.value)) {
      message.error(resolveValidationMessage(backupMessages.cronRequired));
      return;
    }
    if (!validMaxRunMinutes()) {
      message.error(t("databases.backups.protection.maxRunInvalid"));
      return;
    }
    const maxRunSeconds = Math.round(scheduleMaxRunMinutes.value * 60);
    try {
      if (editingScheduleId.value !== null) {
        await backupsStore.editSchedule(resource.value, editingScheduleId.value, {
          cron,
          provider_id: scheduleProviderId.value,
          enabled: scheduleEnabled.value,
          max_run_seconds: maxRunSeconds,
        });
        message.success(t("databases.backups.schedules.updated", { cron }));
      } else {
        await backupsStore.addSchedule(resource.value, {
          cron,
          provider_id: scheduleProviderId.value,
          enabled: scheduleEnabled.value,
          max_run_seconds: maxRunSeconds,
        });
        message.success(t("databases.backups.schedules.saved", { cron }));
      }
      resetScheduleForm();
      if (resourceType.value === "database") {
        await backupsStore.fetchProtection(resourceId.value);
      }
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
      await backupsStore.editSchedule(resource.value, scheduleId, {
        cron,
        enabled,
      });
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
      await backupsStore.removeSchedule(resource.value, scheduleId);
      if (editingScheduleId.value === scheduleId) {
        resetScheduleForm();
      }
      message.success(t("databases.backups.schedules.deleted"));
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  function resetView(): void {
    restoreOpen.value = false;
    restoreCandidate.value = null;
    resetScheduleForm();
    lastRestoreStatus.clear();
    pendingRestoreIds.value = new Set();
    stopBackupPolling();
  }

  watch([resourceType, resourceId], () => {
    resetView();
    if (active.value) {
      void fetchBackupTab();
    }
  });

  // A tab pane mounts already active, so the first load cannot wait for a
  // false->true transition.
  watch(
    active,
    (isActive) => {
      if (isActive) {
        void fetchBackupTab();
      }
      syncBackupPolling();
    },
    { immediate: true },
  );

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
    resource,
    resourceType,
    resourceId,
    active,
    backupProviderId,
    restoreOpen,
    restoreCandidate,
    restoring,
    scheduleCron,
    scheduleProviderId,
    scheduleEnabled,
    scheduleMaxRunMinutes,
    editingScheduleId,
    backups,
    restores,
    hasRunningBackup,
    hasRunningRestore,
    backupProviderOptions,
    scheduleProviderOptions,
    providerLabel,
    protection,
    showProtection,
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
    resetView,
  };
}

export type ResourceBackups = ReturnType<typeof useResourceBackups>;

export const resourceBackupsKey: InjectionKey<ResourceBackups> =
  Symbol("resource-backups");
