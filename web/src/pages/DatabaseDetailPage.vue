<script setup lang="ts">
import {
  NAlert,
  NAvatar,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NInput,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import type { SelectOption } from "naive-ui";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";

import { describeBackupError } from "../api/backups";
import type {
  BackupSchedule,
  BackupTargetKind,
  DatabaseBackup,
  DatabaseRestore,
} from "../api/backups";
import { describeDatabaseError } from "../api/databases";
import type { Database } from "../api/databases";
import DatabaseStatusTag from "../components/DatabaseStatusTag.vue";
import { useMediaQuery } from "../composables/useMediaQuery";
import { useBackupsStore } from "../stores/backups";
import { useDatabasesStore } from "../stores/databases";
import { useServersStore } from "../stores/servers";
import { formatBytes, relativeTime } from "../utils/format";
import { advanceRestoreStatuses } from "../utils/restoreOutcomes";

const route = useRoute();
const router = useRouter();
const message = useMessage();
const databasesStore = useDatabasesStore();
const serversStore = useServersStore();
const backupsStore = useBackupsStore();

const dbId = computed<string>(() => String(route.params.id ?? ""));
const activeTab = ref("overview");
const renameOpen = ref(false);
const renameValue = ref("");
const renaming = ref(false);
const revealed = ref(false);

// The detail page owns its own load state: the list store's flags describe the
// polling list, not this row's fetch, so a failed detail load would otherwise
// render an empty shell with no explanation.
const pageLoading = ref(false);
const pageError = ref<string | null>(null);

const NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/;

/** isNarrow stacks the two-column descriptions on small screens. */
const isNarrow = useMediaQuery("(max-width: 640px)");

/** descColumns renders descriptions in one column below 640px. */
const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));

const database = computed<Database | null>(
  () => databasesStore.databases.find((item) => item.id === dbId.value) ?? null,
);

/** shortId renders the head of the database UUID for the header. */
const shortId = computed<string>(() => dbId.value.slice(0, 8));

/** initials derives a two-letter avatar from the database name. */
const initials = computed<string>(() =>
  (database.value?.name.slice(0, 2) ?? "DB").toUpperCase(),
);

/** engineLabel renders engine + version for the header. */
const engineLabel = computed<string>(() => {
  if (!database.value) {
    return "";
  }
  return database.value.version
    ? `${database.value.engine}:${database.value.version}`
    : database.value.engine;
});

/** serverLabel resolves the node name for the overview. */
const serverLabel = computed<string>(() => {
  if (!database.value) {
    return "—";
  }
  const server = serversStore.servers.find(
    (item) => item.id === database.value?.server_id,
  );
  return server ? `${server.name} · ${server.ip}` : database.value.server_id;
});

/** canStart/canStop/canRestart gate the lifecycle buttons by status. */
const canStart = computed<boolean>(
  () => database.value?.status === "stopped" || database.value?.status === "error",
);

const canStop = computed<boolean>(
  () => database.value?.status === "running",
);

const canRestart = computed<boolean>(
  () => database.value?.status === "running" || database.value?.status === "stopped",
);

const credentials = computed(() =>
  databasesStore.credentialsOf(dbId.value),
);

/** copyText copies a value to the clipboard and confirms with a toast. */
async function copyText(value: string, label: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value);
    message.success(`${label} copied to clipboard`);
  } catch {
    message.error(`Could not copy ${label.toLowerCase()}`);
  }
}

type CredentialField = "username" | "password" | "database" | "root_password";

/** copyCredential copies one cached credential field without narrowing issues. */
function copyCredential(field: CredentialField, label: string): void {
  const value = credentials.value?.[field] ?? "";
  if (value === "") {
    message.error(`No ${label.toLowerCase()} cached yet`);
    return;
  }
  void copyText(value, label);
}

/** Engine default port, used to build the internal connection string. */
const enginePorts: Record<string, number> = {
  postgres: 5432,
  mysql: 3306,
  mariadb: 3306,
  mongodb: 27017,
  redis: 6379,
};

/** URL scheme for each engine's connection string. */
function connectionScheme(engine: string): string {
  switch (engine) {
    case "postgres":
      return "postgresql";
    case "mysql":
      return "mysql";
    case "mariadb":
      return "mariadb";
    case "mongodb":
      return "mongodb";
    case "redis":
      return "redis";
    default:
      return engine;
  }
}

/**
 * connectionString builds the full DSN (with the real password) for the Copy
 * button. The public endpoint is preferred when the database exposes a public
 * port; otherwise the in-network address mirrors the mockup's internal host.
 */
const connectionString = computed<string>(() => {
  const db = database.value;
  const creds = credentials.value;
  if (!db || !creds) {
    return "";
  }
  const scheme = connectionScheme(db.engine);
  const internalPort = enginePorts[db.engine] ?? 0;
  const server = serversStore.servers.find((item) => item.id === db.server_id);
  const host =
    db.public_port > 0
      ? `${server?.ip ?? db.server_id}:${db.public_port}`
      : `gotham-db-${db.name}:${internalPort}`;
  return `${scheme}://${creds.username}:${creds.password}@${host}/${creds.database}`;
});

/** connectionDisplay masks the password for on-screen rendering. */
const connectionDisplay = computed<string>(() =>
  connectionString.value.replace(/:[^:@/]+@/, ":••••••••@"),
);

/** copyConnectionString copies the unmasked DSN. */
function copyConnectionString(): void {
  if (connectionString.value === "") {
    message.error("Credentials are not loaded yet");
    return;
  }
  void copyText(connectionString.value, "Connection string");
}

/** fetchAll loads the row, its credentials and the node list. */
async function fetchAll(): Promise<void> {
  if (!dbId.value) {
    pageError.value = "No database selected.";
    return;
  }
  pageLoading.value = true;
  pageError.value = null;
  try {
    await databasesStore.fetchDatabase(dbId.value);
  } catch (error) {
    pageError.value = describeDatabaseError(error);
    pageLoading.value = false;
    return;
  }
  pageLoading.value = false;
  try {
    await databasesStore.fetchCredentials(dbId.value);
  } catch {
    // The store already exposes the error; the alert renders it.
  }
  void serversStore.fetchServers().catch(() => undefined);
}

/** handleLifecycle runs one start/stop/restart action. */
async function handleLifecycle(
  action: "start" | "stop" | "restart",
): Promise<void> {
  try {
    if (action === "start") {
      await databasesStore.start(dbId.value);
    } else if (action === "stop") {
      await databasesStore.stop(dbId.value);
    } else {
      await databasesStore.restart(dbId.value);
    }
    message.success(
      action === "start" ? "Database started"
        : action === "stop" ? "Database stopped"
        : "Database restarted",
    );
  } catch (error) {
    message.error(describeDatabaseError(error));
  }
}

/** openRename prefills the current name and opens the dialog. */
function openRename(): void {
  renameValue.value = database.value?.name ?? "";
  renameOpen.value = true;
}

/** handleRename submits the PATCH rename. */
async function handleRename(): Promise<void> {
  const name = renameValue.value.trim();
  if (!NAME_PATTERN.test(name)) {
    message.error("Name must be 1-63 characters of letters, digits, ., _ or -.");
    return;
  }
  renaming.value = true;
  try {
    await databasesStore.rename(dbId.value, name);
    message.success(`Database renamed to "${name}"`);
    renameOpen.value = false;
  } catch (error) {
    message.error(describeDatabaseError(error));
  } finally {
    renaming.value = false;
  }
}

/** handleDelete soft-deletes the row and returns to the list. */
async function handleDelete(): Promise<void> {
  const name = database.value?.name ?? dbId.value;
  try {
    await databasesStore.remove(dbId.value);
    message.success(`Database "${name}" deleted · volume kept for 7 days`);
    await router.push({ name: "databases" });
  } catch (error) {
    message.error(describeDatabaseError(error));
  }
}

/** Cron presets offered as chips above the schedule form. */
const CRON_PRESETS = ["0 2 * * *", "0 */6 * * *", "0 3 * * 0"];

/** Kind options for the target form. */
const TARGET_KIND_OPTIONS: SelectOption[] = [
  { label: "S3-compatible", value: "s3" },
  { label: "Local disk", value: "local" },
];

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

interface TargetTestState {
  checking: boolean;
  ok: boolean | null;
  message: string;
}

const targetTests = ref<Record<string, TargetTestState>>({});

let backupPollTimer: ReturnType<typeof setInterval> | null = null;

/** backups renders the runs newest first (the API already orders them). */
const backups = computed<DatabaseBackup[]>(() => {
  const list = backupsStore.backupsOf(dbId.value);
  return [...list].sort((a, b) => b.created_at.localeCompare(a.created_at));
});

/** hasRunningBackup drives the polling loop while a job is in flight. */
const hasRunningBackup = computed<boolean>(() =>
  backups.value.some((item) => item.status === "running"),
);

/** restores renders the durable restore runs newest first. */
const restores = computed<DatabaseRestore[]>(() => {
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

/** statusTagType maps a backup or restore status to a Naive UI tag type. */
function statusTagType(
  status: DatabaseBackup["status"] | DatabaseRestore["status"],
): "success" | "warning" | "error" {
  if (status === "completed") {
    return "success";
  }
  if (status === "failed") {
    return "error";
  }
  return "warning";
}

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

watch(dbId, () => {
  activeTab.value = "overview";
  revealed.value = false;
  restoreOpen.value = false;
  restoreCandidate.value = null;
  resetTargetForm();
  resetScheduleForm();
  lastRestoreStatus.clear();
  pendingRestoreIds.value = new Set();
  stopBackupPolling();
  void fetchAll();
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

onMounted(() => {
  void fetchAll();
  databasesStore.pollDatabases();
});

onUnmounted(() => {
  databasesStore.stopPolling();
  stopBackupPolling();
});
</script>

<template>
  <NSpace vertical :size="16">
    <nav class="breadcrumb" aria-label="Breadcrumb">
      <RouterLink to="/databases">Databases</RouterLink>
      <span class="breadcrumb__sep">/</span>
      <span class="muted mono">{{ database?.name ?? shortId }}</span>
    </nav>

    <NSpin :show="pageLoading">
      <NAlert
        v-if="pageError"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ pageError }}
      </NAlert>
      <NAlert
        v-else-if="!pageLoading && !database"
        type="warning"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        Database not found. It may have been deleted or belong to another
        account.
      </NAlert>

      <div class="page-head">
        <NAvatar round :size="48">{{ initials }}</NAvatar>
        <div class="page-head__title">
          <NSpace align="center" :size="10">
            <NText strong style="font-size: 20px" class="mono">
              {{ database?.name ?? shortId }}
            </NText>
            <DatabaseStatusTag
              v-if="database"
              :status="database.status"
              size="medium"
            />
          </NSpace>
          <NText depth="3" class="mono">{{ dbId }}</NText>
        </div>
        <NSpace class="page-head__actions" align="center" :size="8">
          <NButton
            :disabled="!canStart"
            :loading="databasesStore.acting"
            @click="() => void handleLifecycle('start')"
          >
            Start
          </NButton>
          <NButton
            :disabled="!canStop"
            :loading="databasesStore.acting"
            @click="() => void handleLifecycle('stop')"
          >
            Stop
          </NButton>
          <NButton
            :disabled="!canRestart"
            :loading="databasesStore.acting"
            @click="() => void handleLifecycle('restart')"
          >
            Restart
          </NButton>
          <NButton :disabled="!database" @click="openRename">
            Rename
          </NButton>
          <NPopconfirm @positive-click="() => void handleDelete()">
            <template #trigger>
              <NButton type="error" ghost :loading="databasesStore.acting">
                Delete
              </NButton>
            </template>
            Delete this database? The container is removed from the node, the
            volume {{ database?.volume ?? "" }} is kept for 7 days before
            permanent removal.
          </NPopconfirm>
        </NSpace>
      </div>

      <NTabs v-model:value="activeTab" type="line" animated>
        <NTabPane name="overview" tab="Overview">
          <NSpace vertical :size="16" style="margin-top: 16px">
            <NCard v-if="database" title="Details">
              <NDescriptions :column="descColumns" bordered label-placement="left">
                <NDescriptionsItem label="Engine">
                  <span class="mono">{{ engineLabel }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Node">
                  <span class="mono">{{ serverLabel }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Public port">
                  <span class="mono">
                    {{
                      database.public_port > 0
                        ? database.public_port
                        : "off · internal network only"
                    }}
                  </span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Volume">
                  <span class="mono">{{ database.volume }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Container">
                  <span class="mono">{{ database.container_id || "—" }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Created">
                  {{ relativeTime(database.created_at) }}
                </NDescriptionsItem>
              </NDescriptions>
            </NCard>

            <NCard title="Credentials">
              <template #header-extra>
                <NText depth="3">Stored encrypted · owner only</NText>
              </template>
              <NAlert
                v-if="databasesStore.credentialsError"
                type="error"
                :show-icon="true"
                style="margin-bottom: 12px"
              >
                {{ databasesStore.credentialsError }}
              </NAlert>
              <NSpin :show="databasesStore.credentialsLoading">
                <NSpace v-if="credentials" vertical :size="12">
                  <div class="credential-row">
                    <NText depth="3">Username</NText>
                    <NText class="mono grow">{{ credentials.username }}</NText>
                    <NButton
                      size="small"
                      secondary
                      @click="() => copyCredential('username', 'Username')"
                    >
                      Copy
                    </NButton>
                  </div>
                  <div class="credential-row">
                    <NText depth="3">Password</NText>
                    <NText class="mono grow">
                      {{ revealed ? credentials.password : "••••••••••••" }}
                    </NText>
                    <NButton
                      size="small"
                      secondary
                      @click="revealed = !revealed"
                    >
                      {{ revealed ? "Hide" : "Reveal" }}
                    </NButton>
                    <NButton
                      size="small"
                      secondary
                      @click="() => copyCredential('password', 'Password')"
                    >
                      Copy
                    </NButton>
                  </div>
                  <div class="credential-row">
                    <NText depth="3">Database</NText>
                    <NText class="mono grow">{{ credentials.database }}</NText>
                    <NButton
                      size="small"
                      secondary
                      @click="() => copyCredential('database', 'Database')"
                    >
                      Copy
                    </NButton>
                  </div>
                  <div v-if="credentials.root_password" class="credential-row">
                    <NText depth="3">Root password</NText>
                    <NText class="mono grow">
                      {{ revealed ? credentials.root_password : "••••••••••••" }}
                    </NText>
                    <NButton
                      size="small"
                      secondary
                      @click="() => copyCredential('root_password', 'Root password')"
                    >
                      Copy
                    </NButton>
                  </div>
                  <div v-if="connectionString" class="connection-block">
                    <NText depth="3" class="connection-label">
                      Connection string
                    </NText>
                    <pre class="connection-string"><code>{{ connectionDisplay }}</code></pre>
                    <NButton size="small" secondary @click="copyConnectionString">
                      Copy connection string
                    </NButton>
                  </div>
                </NSpace>
                <NEmpty
                  v-else-if="!databasesStore.credentialsLoading"
                  description="No credentials cached — they load automatically with the page."
                />
              </NSpin>
            </NCard>
          </NSpace>
        </NTabPane>

        <NTabPane name="backups" tab="Backups">
          <NSpace vertical :size="16" style="margin-top: 16px">
            <NCard title="Backups">
              <template #header-extra>
                <NSpace align="center" :size="8">
                  <NSelect
                    v-model:value="backupTargetId"
                    :options="backupTargetOptions"
                    placeholder="Destination"
                    aria-label="Backup destination"
                    style="width: 220px"
                  />
                  <NButton
                    size="small"
                    secondary
                    :disabled="backupsStore.backupsLoading"
                    @click="() => void fetchBackupTab()"
                  >
                    Refresh
                  </NButton>
                  <NPopconfirm @positive-click="() => void handleBackupNow()">
                    <template #trigger>
                      <NButton
                        size="small"
                        type="primary"
                        :loading="backupsStore.backupsActing"
                      >
                        Backup now
                      </NButton>
                    </template>
                    A backup stops this database while the dump runs, so it is
                    briefly unavailable. Continue?
                  </NPopconfirm>
                </NSpace>
              </template>
              <NAlert
                v-if="backupsStore.backupsError"
                type="error"
                :show-icon="true"
                style="margin-bottom: 12px"
              >
                {{ backupsStore.backupsError }}
              </NAlert>
              <NAlert
                v-if="backupsStore.targets.length === 0"
                type="info"
                :show-icon="false"
                style="margin-bottom: 12px"
              >
                No S3 target configured — backups are stored on the control
                plane disk. Add an S3-compatible target below to keep them
                off-node.
              </NAlert>
              <NSpin :show="backupsStore.backupsLoading">
                <NSpace
                  v-if="backups.length > 0"
                  vertical
                  :size="12"
                  style="width: 100%"
                >
                  <div
                    v-for="backup in backups"
                    :key="backup.id"
                    class="backup-row"
                  >
                    <div class="backup-row__main">
                      <NSpace align="center" :size="8">
                        <NTag :type="statusTagType(backup.status)" size="small">
                          {{ backup.status }}
                        </NTag>
                        <NTag size="small" :bordered="false">
                          {{ backup.type }}
                        </NTag>
                        <NText class="mono" depth="3">
                          {{
                            backup.status === "running"
                              ? "size pending"
                              : formatBytes(backup.size)
                          }}
                        </NText>
                      </NSpace>
                      <NText class="mono backup-row__location">
                        {{ backup.location || "Dump in progress…" }}
                      </NText>
                      <NText
                        v-if="backup.error"
                        type="error"
                        class="backup-row__error"
                      >
                        {{ backup.error }}
                      </NText>
                      <NText depth="3">
                        <span :title="backup.created_at">
                          {{ relativeTime(backup.created_at) }}
                        </span>
                        <span v-if="backup.schedule_id" class="mono">
                          · scheduled
                        </span>
                      </NText>
                    </div>
                    <NSpace class="backup-row__actions" align="center" :size="8">
                      <NButton
                        size="small"
                        secondary
                        :aria-label="`Restore backup from ${relativeTime(backup.created_at)}`"
                        :disabled="backup.status !== 'completed'"
                        @click="openRestore(backup)"
                      >
                        Restore
                      </NButton>
                      <NPopconfirm
                        @positive-click="() => void handleDeleteBackup(backup.id)"
                      >
                        <template #trigger>
                          <NButton
                            size="small"
                            type="error"
                            ghost
                            :aria-label="`Delete backup from ${relativeTime(backup.created_at)}`"
                            :disabled="backup.status === 'running'"
                          >
                            Delete
                          </NButton>
                        </template>
                        Delete this backup? The stored artifact goes first,
                        then the row. This cannot be undone.
                      </NPopconfirm>
                    </NSpace>
                  </div>
                </NSpace>
                <NEmpty
                  v-else-if="!backupsStore.backupsLoading"
                  description="No backups yet"
                >
                  <template #extra>
                    <p class="empty-hint">
                      Queue a manual backup above, or add a schedule so the
                      control plane dumps this database automatically.
                    </p>
                  </template>
                </NEmpty>
              </NSpin>
            </NCard>

            <NCard v-if="restores.length > 0" title="Restore history">
              <template #header-extra>
                <NText depth="3">Durable result of each queued restore</NText>
              </template>
              <NAlert
                v-if="backupsStore.restoresError"
                type="error"
                :show-icon="true"
                style="margin-bottom: 12px"
              >
                {{ backupsStore.restoresError }}
              </NAlert>
              <NSpin :show="backupsStore.restoresLoading">
                <NSpace vertical :size="12" style="width: 100%">
                  <div
                    v-for="restore in restores"
                    :key="restore.id"
                    class="backup-row"
                  >
                    <div class="backup-row__main">
                      <NSpace align="center" :size="8">
                        <NTag :type="statusTagType(restore.status)" size="small">
                          {{ restore.status }}
                        </NTag>
                        <NText class="mono" depth="3">
                          backup {{ restore.backup_id.slice(0, 8) }}
                        </NText>
                      </NSpace>
                      <NText
                        v-if="restore.error"
                        type="error"
                        class="backup-row__error"
                      >
                        {{ restore.error }}
                      </NText>
                      <NText depth="3">
                        <span :title="restore.created_at">
                          {{ relativeTime(restore.created_at) }}
                        </span>
                        <span v-if="restore.finished_at">
                          · finished
                          <span :title="restore.finished_at">
                            {{ relativeTime(restore.finished_at) }}
                          </span>
                        </span>
                      </NText>
                    </div>
                  </div>
                </NSpace>
              </NSpin>
            </NCard>

            <NCard title="Schedules">
              <template #header-extra>
                <NText depth="3">Cron in the control plane</NText>
              </template>
              <NAlert
                v-if="backupsStore.schedulesError"
                type="error"
                :show-icon="true"
                style="margin-bottom: 12px"
              >
                {{ backupsStore.schedulesError }}
              </NAlert>
              <NSpin :show="backupsStore.schedulesLoading">
                <NSpace
                  v-if="backupsStore.schedulesOf(dbId).length > 0"
                  vertical
                  :size="12"
                  style="width: 100%"
                >
                  <div
                    v-for="schedule in backupsStore.schedulesOf(dbId)"
                    :key="schedule.id"
                    class="backup-row"
                  >
                    <div class="backup-row__main">
                      <NSpace align="center" :size="8">
                        <NText class="mono" strong>
                          {{ schedule.cron }}
                        </NText>
                        <NTag size="small" :bordered="false">
                          {{ targetLabel(schedule.target_id) }}
                        </NTag>
                      </NSpace>
                      <NText depth="3">
                        Next run
                        <span :title="schedule.next_run_at">
                          {{ relativeTime(schedule.next_run_at) }}
                        </span>
                        <span v-if="schedule.last_run_at">
                          · last
                          <span :title="schedule.last_run_at">
                            {{ relativeTime(schedule.last_run_at) }}
                          </span>
                        </span>
                      </NText>
                    </div>
                    <NSpace class="backup-row__actions" align="center" :size="8">
                      <NSwitch
                        :value="schedule.enabled"
                        :aria-label="`Enable schedule ${schedule.cron}`"
                        :loading="backupsStore.schedulesActing"
                        @update:value="
                          (enabled: boolean) =>
                            void handleToggleSchedule(
                              schedule.id,
                              schedule.cron,
                              enabled,
                            )
                        "
                      >
                        <template #checked>On</template>
                        <template #unchecked>Off</template>
                      </NSwitch>
                      <NButton
                        size="small"
                        secondary
                        :aria-label="`Edit schedule ${schedule.cron}`"
                        @click="openScheduleEdit(schedule)"
                      >
                        Edit
                      </NButton>
                      <NPopconfirm
                        @positive-click="
                          () => void handleDeleteSchedule(schedule.id)
                        "
                      >
                        <template #trigger>
                          <NButton
                            size="small"
                            type="error"
                            ghost
                            :aria-label="`Delete schedule ${schedule.cron}`"
                          >
                            Delete
                          </NButton>
                        </template>
                        Delete this schedule? Past backups stay untouched.
                      </NPopconfirm>
                    </NSpace>
                  </div>
                </NSpace>
                <NEmpty
                  v-else-if="!backupsStore.schedulesLoading"
                  description="No schedules yet — automatic backups are off"
                />
              </NSpin>
              <div class="schedule-form">
                <NText strong>
                  {{ editingScheduleId === null ? "New schedule" : "Edit schedule" }}
                </NText>
                <NSpace align="center" :size="8">
                  <NButton
                    v-for="preset in CRON_PRESETS"
                    :key="preset"
                    size="small"
                    secondary
                    @click="scheduleCron = preset"
                  >
                    {{ preset }}
                  </NButton>
                </NSpace>
                <div class="schedule-form__row">
                  <NInput
                    v-model:value="scheduleCron"
                    class="mono grow"
                    placeholder="0 2 * * *"
                    aria-label="Cron expression"
                  />
                  <NSelect
                    v-model:value="scheduleTargetId"
                    :options="scheduleTargetOptions"
                    placeholder="Destination"
                    aria-label="Schedule destination"
                    style="width: 220px"
                  />
                  <NSwitch
                    v-model:value="scheduleEnabled"
                    aria-label="Enable the new schedule"
                  >
                    <template #checked>On</template>
                    <template #unchecked>Off</template>
                  </NSwitch>
                  <NButton
                    type="primary"
                    :loading="backupsStore.schedulesActing"
                    :disabled="scheduleCron.trim() === ''"
                    @click="() => void handleCreateSchedule()"
                  >
                    {{
                      editingScheduleId === null ? "Add schedule" : "Save schedule"
                    }}
                  </NButton>
                  <NButton
                    v-if="editingScheduleId !== null"
                    @click="resetScheduleForm()"
                  >
                    Cancel
                  </NButton>
                </div>
              </div>
            </NCard>

            <NCard title="Backup targets">
              <template #header-extra>
                <NText depth="3">S3-compatible storage</NText>
              </template>
              <NAlert
                v-if="backupsStore.targetsError"
                type="error"
                :show-icon="true"
                style="margin-bottom: 12px"
              >
                {{ backupsStore.targetsError }}
              </NAlert>
              <NSpin :show="backupsStore.targetsLoading">
                <NSpace
                  v-if="backupsStore.targets.length > 0"
                  vertical
                  :size="12"
                  style="width: 100%"
                >
                  <div
                    v-for="target in backupsStore.targets"
                    :key="target.id"
                    class="backup-row"
                  >
                    <div class="backup-row__main">
                      <NSpace align="center" :size="8">
                        <NText strong>{{ target.name }}</NText>
                        <NTag size="small" :bordered="false">
                          {{ target.kind }}
                        </NTag>
                        <NTag
                          v-if="target.has_credentials"
                          size="small"
                          type="success"
                        >
                          Credentials configured
                        </NTag>
                        <NTag v-else size="small" type="warning">
                          No credentials
                        </NTag>
                      </NSpace>
                      <NText class="mono backup-row__location" depth="3">
                        {{
                          target.kind === "s3"
                            ? `${target.endpoint ?? ""} · ${target.bucket ?? ""}${target.prefix ? ` · ${target.prefix}` : ""}`
                            : "control plane disk"
                        }}
                      </NText>
                      <NText
                        v-if="
                          targetTests[target.id] &&
                          !targetTests[target.id].checking
                        "
                        :type="
                          targetTests[target.id].ok ? 'success' : 'error'
                        "
                      >
                        {{ targetTests[target.id].message }}
                      </NText>
                    </div>
                    <NSpace class="backup-row__actions" align="center" :size="8">
                      <NButton
                        size="small"
                        secondary
                        :aria-label="`Test target ${target.name}`"
                        :loading="targetTests[target.id]?.checking"
                        @click="() => void handleTestTarget(target.id)"
                      >
                        Test
                      </NButton>
                      <NButton
                        size="small"
                        secondary
                        :aria-label="`Edit target ${target.name}`"
                        @click="openTargetEdit(target.id)"
                      >
                        Edit
                      </NButton>
                      <NPopconfirm
                        @positive-click="
                          () => void handleDeleteTarget(target.id, target.name)
                        "
                      >
                        <template #trigger>
                          <NButton
                            size="small"
                            type="error"
                            ghost
                            :aria-label="`Delete target ${target.name}`"
                          >
                            Delete
                          </NButton>
                        </template>
                        Delete target "{{ target.name }}"? Past backups keep
                        their location but can no longer be read back from
                        this target.
                      </NPopconfirm>
                    </NSpace>
                  </div>
                </NSpace>
                <NEmpty
                  v-else-if="!backupsStore.targetsLoading"
                  description="No backup targets yet"
                >
                  <template #extra>
                    <p class="empty-hint">
                      Backups fall back to the control plane disk until an
                      S3-compatible target is configured.
                    </p>
                  </template>
                </NEmpty>
              </NSpin>
              <div class="target-form">
                <NText strong>
                  {{ targetEditingId === null ? "New target" : "Edit target" }}
                </NText>
                <div class="target-form__grid">
                  <NInput
                    v-model:value="targetName"
                    placeholder="Target name"
                    aria-label="Target name"
                  />
                  <NSelect
                    v-model:value="targetKind"
                    :options="TARGET_KIND_OPTIONS"
                    aria-label="Target kind"
                  />
                </div>
                <template v-if="targetKind === 's3'">
                  <div class="target-form__grid">
                    <NInput
                      v-model:value="targetEndpoint"
                      class="mono"
                      placeholder="https://…endpoint"
                      aria-label="Endpoint"
                    />
                    <NInput
                      v-model:value="targetRegion"
                      class="mono"
                      placeholder="Region (e.g. auto)"
                      aria-label="Region"
                    />
                  </div>
                  <div class="target-form__grid">
                    <NInput
                      v-model:value="targetBucket"
                      class="mono"
                      placeholder="Bucket"
                      aria-label="Bucket"
                    />
                    <NInput
                      v-model:value="targetPrefix"
                      class="mono"
                      placeholder="Key prefix (optional)"
                      aria-label="Key prefix"
                    />
                  </div>
                  <div class="target-form__grid">
                    <NInput
                      v-model:value="targetAccessKey"
                      type="password"
                      class="mono"
                      :placeholder="
                        targetEditingId === null
                          ? 'Access key'
                          : 'Access key · blank keeps stored keys'
                      "
                      aria-label="Access key"
                    />
                    <NInput
                      v-model:value="targetSecretKey"
                      type="password"
                      class="mono"
                      :placeholder="
                        targetEditingId === null
                          ? 'Secret key'
                          : 'Secret key · blank keeps stored keys'
                      "
                      aria-label="Secret key"
                    />
                  </div>
                  <NText depth="3">
                    Secrets are sealed on the server and never shown back —
                    leave the key fields blank to keep the stored ones.
                  </NText>
                </template>
                <NSpace justify="end" :size="8">
                  <NButton
                    v-if="targetEditingId !== null"
                    @click="resetTargetForm()"
                  >
                    Cancel
                  </NButton>
                  <NButton
                    type="primary"
                    :loading="backupsStore.targetsActing"
                    @click="() => void handleSaveTarget()"
                  >
                    {{
                      targetEditingId === null ? "Add target" : "Save target"
                    }}
                  </NButton>
                </NSpace>
              </div>
            </NCard>
          </NSpace>
        </NTabPane>
      </NTabs>
    </NSpin>

    <NModal
      v-model:show="renameOpen"
      preset="card"
      title="Rename database"
      style="width: 480px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NText depth="3">
          Only the display name changes — the container, volume and credentials
          stay untouched.
        </NText>
        <NInput
          v-model:value="renameValue"
          class="mono"
          placeholder="New database name"
          @keyup.enter="() => void handleRename()"
        />
        <NSpace justify="end" :size="8">
          <NButton @click="renameOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="renaming"
            :disabled="!NAME_PATTERN.test(renameValue.trim())"
            @click="() => void handleRename()"
          >
            Rename
          </NButton>
        </NSpace>
      </NSpace>
    </NModal>

    <NModal
      v-model:show="restoreOpen"
      preset="card"
      title="Restore database"
      style="width: 520px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NText>
          Restore
          <NText strong class="mono">{{ database?.name ?? shortId }}</NText>
          from the backup
          <NText strong class="mono">
            {{ restoreCandidate?.location || restoreCandidate?.id }}
          </NText>
          ({{ relativeTime(restoreCandidate?.created_at) }},
          {{ formatBytes(restoreCandidate?.size ?? 0) }})?
        </NText>
        <NAlert type="warning" :show-icon="true">
          The restore runs in a temporary container and overwrites the current
          data of this database. This cannot be undone — back up first if the
          live data still matters.
        </NAlert>
        <NSpace justify="end" :size="8">
          <NButton @click="restoreOpen = false">Cancel</NButton>
          <NButton
            type="warning"
            :loading="restoring"
            @click="() => void handleRestoreConfirm()"
          >
            Restore · overwrite data
          </NButton>
        </NSpace>
      </NSpace>
    </NModal>
  </NSpace>
</template>

<style scoped>
.breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
}

.breadcrumb__sep {
  color: var(--meta);
}

.muted {
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}

.grow {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.page-head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.page-head__title {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  flex: 1;
  min-width: 0;
}

.page-head__actions {
  margin-left: auto;
  flex-shrink: 0;
}

.credential-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.connection-block {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--space-2);
}

.connection-label {
  font-size: var(--text-sm);
}

.connection-string {
  margin: 0;
  width: 100%;
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-3);
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

.connection-string code {
  font-family: inherit;
}

.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0;
  max-width: 62ch;
}

.backup-row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-3) 0;
  border-bottom: 1px solid var(--border);
}

.backup-row:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

.backup-row__main {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  flex: 1;
  min-width: 0;
}

.backup-row__location {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.backup-row__error {
  overflow-wrap: anywhere;
}

.backup-row__actions {
  flex-shrink: 0;
}

.schedule-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  margin-top: var(--space-4);
  padding-top: var(--space-4);
  border-top: 1px dashed var(--border);
}

.schedule-form__row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.target-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  margin-top: var(--space-4);
  padding-top: var(--space-4);
  border-top: 1px dashed var(--border);
}

.target-form__grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-2);
}

@media (max-width: 640px) {
  .target-form__grid {
    grid-template-columns: 1fr;
  }

  .backup-row {
    flex-direction: column;
  }
}
</style>
