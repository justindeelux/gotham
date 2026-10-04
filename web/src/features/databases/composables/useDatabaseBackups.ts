import { useMessage } from "naive-ui";
import type { SelectOption } from "naive-ui";
import { computed, onUnmounted, ref, watch } from "vue";
import type { InjectionKey, Ref } from "vue";

import { describeBackupError } from "@/features/databases/api/backups";
import type {
  BackupSchedule,
  BackupTargetKind,
  DatabaseBackup,
} from "@/features/databases/api/backups";
import { useBackupsStore } from "@/features/databases/stores/backups";
import { advanceRestoreStatuses } from "@/features/databases/utils/restoreOutcomes";

export interface TargetTestState {
  checking: boolean;
  ok: boolean | null;
  message: string;
}

/** Cron presets offered as chips above the schedule form. */
export const CRON_PRESETS = ["0 2 * * *", "0 */6 * * *", "0 3 * * 0"];

/** Kind options for the target form. */
export const TARGET_KIND_OPTIONS: SelectOption[] = [
  { label: "S3-compatible", value: "s3" },
  { label: "Local disk", value: "local" },
];

/**
 * useDatabaseBackups owns the backups tab state for one database: runs,
 * restores, schedules, targets, the running-job poll and all tab mutations.
 * The tab component creates one instance and provides it to the tab cards.
 */
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

  /** backups renders the runs newest first (the API already orders them). */
  const backups = computed(() => {
    const list = backupsStore.backupsOf(dbId.value);
    return [...list].sort((a, b) => b.created_at.localeCompare(a.created_at));
  });

  /** hasRunningBackup drives the polling loop while a job is in flight. */
  const hasRunningBackup = computed<boolean>(() =>
    backups.value.some((item) => item.status === "running"),
  );

  /** restores renders the durable restore runs newest first. */
  const restores = computed(() => {
    const list = backupsStore.restoresOf(dbId.value);
    return [...list].sort((a, b) => b.created_at.localeCompare(a.created_at));
  });

  // Restores queued in this tab but not yet observed in a list response. They
  // keep the poll alive when the first post-queue list fetch fails, and are
  // dropped once a list reports them.
  const pendingRestoreIds = ref<Set<string>>(new Set());

  /** hasRunningRestore keeps the poll alive until a queued restore finishes. */
  const hasRunningRestore = computed<boolean>(
    () =>
      pendingRestoreIds.value.size > 0 ||
      restores.value.some((item) => item.status === "running"),
  );

  // Last status seen per restore id, so a running→terminal transition can toast
  // exactly once. Kept outside reactivity: it is bookkeeping, not UI state.
  const lastRestoreStatus = new Map<string, string>();

  // Monotonic token for the restore poll: it gates the poll's own notify, so an
  // older overlapping poll cannot announce a status a newer one already handled.
  // The store write itself is unconditional; the monotonic seen-status map in
  // advanceRestoreStatuses is what stops a stale list from re-arming a toast.
  let restorePollEpoch = 0;

  /** backupTargetOptions lists the destination choices for backup-now. */
  const backupTargetOptions = computed<SelectOption[]>(() => {
    const options: SelectOption[] = [
      { label: "Local disk (default)", value: "" },
    ];
    for (const item of backupsStore.targets) {
      options.push({
        label: `${item.name} · ${item.bucket || item.kind}`,
        value: item.id,
      });
    }
    return options;
  });

  /** scheduleTargetOptions reuses the same choices for scheduled runs. */
  const scheduleTargetOptions = computed<SelectOption[]>(
    () => backupTargetOptions.value,
  );

  /** targetLabel resolves a schedule's destination for the list rows. */
  function targetLabel(targetId: string | undefined): string {
    if (!targetId) {
      return "local disk";
    }
    return backupsStore.targetOf(targetId)?.name ?? "deleted target";
  }

  /** fetchBackupTab loads backups, restores, schedules and targets for the tab. */
  async function fetchBackupTab(): Promise<void> {
    if (!dbId.value) {
      return;
    }
    await Promise.allSettled([
      backupsStore.fetchBackups(dbId.value),
      backupsStore.fetchRestores(dbId.value),
      backupsStore.fetchSchedules(dbId.value),
      backupsStore.fetchTargets(),
    ]);
    // Seed the status of every restore the first fetch returned, so a restore
    // that was already running when the tab opened still toasts on completion
    // instead of being skipped by the first-poll "previous === undefined" case.
    // advanceRestoreStatuses is monotonic, so a slow stale GET cannot re-arm a
    // toast a poll already fired.
    notifyRestoreOutcomes();
  }

  /** stopBackupPolling clears the running-job refresh interval. */
  function stopBackupPolling(): void {
    // No epoch bump here: stopping because a restore just completed must not
    // suppress the poll that observed it (that poll still has to notify). The
    // poll epoch is bumped only when a new poll starts.
    if (backupPollTimer !== null) {
      clearInterval(backupPollTimer);
      backupPollTimer = null;
    }
  }

  /** reconcilePendingRestores drops queued ids once a list reports them. */
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

  /** notifyRestoreOutcomes toasts a restore that just reached a terminal state. */
  function notifyRestoreOutcomes(): void {
    reconcilePendingRestores();
    for (const outcome of advanceRestoreStatuses(
      lastRestoreStatus,
      restores.value,
    )) {
      if (outcome.status === "completed") {
        message.success("Restore completed");
      } else {
        message.error(outcome.error || "Restore failed");
      }
    }
  }

  /** pollRunningJobs refreshes backups and restores while either has a live job. */
  async function pollRunningJobs(id: string): Promise<void> {
    const epoch = ++restorePollEpoch;
    await Promise.allSettled([
      backupsStore.refreshBackups(id),
      backupsStore.refreshRestores(id),
    ]);
    // An older overlapping poll was superseded: ignore its notify. The completion
    // itself still fires from the watcher below, which is not epoch-gated.
    if (epoch !== restorePollEpoch) {
      return;
    }
    notifyRestoreOutcomes();
  }

  /** syncBackupPolling refreshes every 5s while a backup or restore is running. */
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

  /** handleBackupNow queues a manual backup to the selected destination. */
  async function handleBackupNow(): Promise<void> {
    try {
      await backupsStore.backupNow(dbId.value, backupTargetId.value);
      message.success("Backup queued · the dump runs in a temporary container");
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  /** handleDeleteBackup removes one run (artifact first, then the row). */
  async function handleDeleteBackup(backupId: string): Promise<void> {
    try {
      await backupsStore.removeBackup(dbId.value, backupId);
      message.success("Backup deleted");
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  /** openRestore names the backup and opens the overwrite confirmation. */
  function openRestore(backup: DatabaseBackup): void {
    restoreCandidate.value = backup;
    restoreOpen.value = true;
  }

  /** handleRestoreConfirm queues the restore after explicit confirmation. */
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
      message.success("Restore queued · the database is stopped while it runs");
      restoreOpen.value = false;
      restoreCandidate.value = null;
      // Seed the queued status from the 202 answer: the row can reach a terminal
      // state before the first list fetch, and the transition would otherwise be
      // missed. The pending id also keeps polling alive if that fetch fails.
      if (queued.restore_id) {
        lastRestoreStatus.set(queued.restore_id, queued.status);
        pendingRestoreIds.value = new Set(pendingRestoreIds.value).add(
          queued.restore_id,
        );
      }
      // refreshRestores swallows a failed read (the queue already succeeded), so
      // a transient list error cannot hide the "queued" result or skip the poll.
      await backupsStore.refreshRestores(dbId.value);
      notifyRestoreOutcomes();
    } catch (error) {
      message.error(describeBackupError(error));
    } finally {
      syncBackupPolling();
      restoring.value = false;
    }
  }

  /** resetScheduleForm clears the schedule editor back to a create. */
  function resetScheduleForm(): void {
    editingScheduleId.value = null;
    scheduleCron.value = "";
    scheduleTargetId.value = "";
    scheduleEnabled.value = true;
  }

  /** openScheduleEdit prefills the editor from an existing schedule. */
  function openScheduleEdit(schedule: BackupSchedule): void {
    editingScheduleId.value = schedule.id;
    scheduleCron.value = schedule.cron;
    // A schedule may outlive its target; only prefill an id that still exists,
    // or saving would PATCH a dead target and 404.
    scheduleTargetId.value = backupsStore.targetOf(schedule.target_id)
      ? schedule.target_id ?? ""
      : "";
    scheduleEnabled.value = schedule.enabled;
  }

  /** handleCreateSchedule creates or updates one cron entry. */
  async function handleCreateSchedule(): Promise<void> {
    const cron = scheduleCron.value.trim();
    if (cron === "") {
      message.error("Cron expression is required, e.g. 0 2 * * *.");
      return;
    }
    try {
      if (editingScheduleId.value !== null) {
        await backupsStore.editSchedule(dbId.value, editingScheduleId.value, {
          cron,
          target_id: scheduleTargetId.value,
          enabled: scheduleEnabled.value,
        });
        message.success(`Schedule updated · next run computed from ${cron}`);
      } else {
        await backupsStore.addSchedule(dbId.value, {
          cron,
          target_id: scheduleTargetId.value,
          enabled: scheduleEnabled.value,
        });
        message.success(`Schedule saved · next run computed from ${cron}`);
      }
      resetScheduleForm();
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  /**
   * handleToggleSchedule flips one schedule. It sends only cron and enabled: the
   * stored target is left untouched, so a schedule whose target was deleted can
   * still be paused instead of failing the PATCH with a 404.
   */
  async function handleToggleSchedule(
    scheduleId: string,
    cron: string,
    enabled: boolean,
  ): Promise<void> {
    try {
      await backupsStore.editSchedule(dbId.value, scheduleId, { cron, enabled });
      message.success(enabled ? "Schedule enabled" : "Schedule paused");
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  /** handleDeleteSchedule removes one cron entry. */
  async function handleDeleteSchedule(scheduleId: string): Promise<void> {
    try {
      await backupsStore.removeSchedule(dbId.value, scheduleId);
      if (editingScheduleId.value === scheduleId) {
        resetScheduleForm();
      }
      message.success("Schedule deleted");
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  /** resetTargetForm clears the target editor back to an s3 create. */
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

  /** openTargetEdit prefills the editor; secrets stay blank to keep them. */
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
    // The last test result described the stored configuration; editing may
    // change it, so a stale "connected" must not survive the edit.
    delete targetTests.value[targetId];
  }

  /** handleSaveTarget creates or updates a target from the editor. */
  async function handleSaveTarget(): Promise<void> {
    const name = targetName.value.trim();
    if (name === "") {
      message.error("Target name is required.");
      return;
    }
    if (targetKind.value === "s3") {
      if (targetEndpoint.value.trim() === "" || targetBucket.value.trim() === "") {
        message.error("Endpoint and bucket are required for an S3 target.");
        return;
      }
      if (
        targetEditingId.value === null &&
        (targetAccessKey.value === "" || targetSecretKey.value === "")
      ) {
        message.error("Access key and secret key are required for a new S3 target.");
        return;
      }
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
        message.success(`Target "${name}" saved · credentials sealed`);
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
        message.success(`Target "${name}" updated`);
      }
      resetTargetForm();
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  /** handleDeleteTarget removes one target the caller owns. */
  async function handleDeleteTarget(targetId: string, name: string): Promise<void> {
    try {
      await backupsStore.removeTarget(targetId);
      // Drop any selection or editor reference to the deleted target, or the
      // next backup/schedule action would PATCH a dead id and 404.
      if (backupTargetId.value === targetId) {
        backupTargetId.value = "";
      }
      if (scheduleTargetId.value === targetId) {
        scheduleTargetId.value = "";
      }
      delete targetTests.value[targetId];
      message.success(`Target "${name}" deleted`);
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  /** handleTestTarget checks one target's connection and shows the answer. */
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
        message: error instanceof Error ? error.message : "Test failed",
      };
    }
  }

  /** resetView clears the per-database backup UI when the route id changes. */
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
    // Notify before the stop: a completion flips this to false and stops the
    // interval, and the toast must still fire for the poll that observed it.
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
    handleTestTarget,
    resetView,
  };
}

export type DatabaseBackups = ReturnType<typeof useDatabaseBackups>;

/** Injection key for the page-owned backups state shared with tab cards. */
export const databaseBackupsKey: InjectionKey<DatabaseBackups> =
  Symbol("database-backups");
