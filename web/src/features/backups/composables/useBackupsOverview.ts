import { useMessage } from "naive-ui";
import type { SelectOption } from "naive-ui";
import { computed, onUnmounted, ref } from "vue";

import { listApplications } from "@/features/applications/api/applications";
import type { Application } from "@/features/applications/api/applications";
import { applicationBrand, brandFor } from "@/features/add-resource";
import type { BrandMark } from "@/features/add-resource";
import { useAuthStore } from "@/features/auth";
import { describeBackupError } from "@/features/backups/api/backups";
import type {
  BackupRun,
  BackupSchedule,
  ResourceRef,
  ResourceType,
} from "@/features/backups/api/backups";
import {
  CRON_PRESETS,
  matchesBackupFilters,
  resourceTypeLabel,
} from "@/features/backups/utils/backupStatus";
import {
  backupMessages,
  isCronPresent,
  isKeepLastValid,
} from "@/features/backups/schemas/backups";
import { useBackupsStore } from "@/features/backups/stores/backups";
import { listDatabases } from "@/features/databases/api/databases";
import type { Database } from "@/features/databases/api/databases";
import { listServices } from "@/features/services/api/services";
import type { Service } from "@/features/services/api/services";
import { i18n, resolveValidationMessage } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

// Volume drivers land in GOTHAM-129; until then only databases can be queued.
export const SUPPORTED_RESOURCE_TYPES: ResourceType[] = ["database"];

export interface ResolvedResource {
  id: string;
  type: ResourceType;
  name: string;
  link: string | null;
  brand: BrandMark | null;
}

function resourceKeyOf(type: ResourceType, id: string): string {
  return `${type}:${id}`;
}

export function useBackupsOverview() {
  const message = useMessage();
  const backupsStore = useBackupsStore();
  const authStore = useAuthStore();
  const isOwner = computed(() => authStore.isOwner);

  const databases = ref<Database[]>([]);
  const applications = ref<Application[]>([]);
  const services = ref<Service[]>([]);

  const typeFilter = ref<"all" | ResourceType>("all");
  const statusFilter = ref<"all" | BackupRun["status"]>("all");

  const backupDialogOpen = ref(false);
  const newResourceKey = ref("");
  const newProviderId = ref("");

  const scheduleDialogOpen = ref(false);
  const scheduleResourceKey = ref("");
  const scheduleCron = ref(CRON_PRESETS[0]);
  const scheduleProviderId = ref("");
  const scheduleEnabled = ref(true);
  const scheduleMaxRunMinutes = ref(120);
  const scheduleKeepLast = ref(7);

  const restoreOpen = ref(false);
  const restoreCandidate = ref<BackupRun | null>(null);
  const restoring = ref(false);

  let pollTimer: ReturnType<typeof setInterval> | null = null;

  const resources = computed<Record<string, ResolvedResource>>(() => {
    const map: Record<string, ResolvedResource> = {};
    for (const item of databases.value) {
      map[resourceKeyOf("database", item.id)] = {
        id: item.id,
        type: "database",
        name: item.name,
        link: `/projects/${item.project_id}/environments/${item.environment_id}/databases/${item.id}`,
        brand: brandFor(item.engine, item.name),
      };
    }
    for (const item of applications.value) {
      map[resourceKeyOf("application", item.id)] = {
        id: item.id,
        type: "application",
        name: item.name,
        link: `/projects/${item.project_id}/environments/${item.environment_id}/applications/${item.id}`,
        brand: applicationBrand(item.source_type, item.provider, item.name),
      };
    }
    for (const item of services.value) {
      map[resourceKeyOf("service", item.id)] = {
        id: item.id,
        type: "service",
        name: item.name,
        link: `/projects/${item.project_id}/environments/${item.environment_id}/services/${item.id}`,
        brand: brandFor("compose", item.name),
      };
    }
    return map;
  });

  function resolveResource(type: ResourceType, id: string): ResolvedResource {
    return (
      resources.value[resourceKeyOf(type, id)] ?? {
        id,
        type,
        name: t("backups.resources.unknown", { id: id.slice(0, 8) }),
        link: null,
        brand: null,
      }
    );
  }

  const filteredBackups = computed(() =>
    [...backupsStore.allBackups]
      .filter((item) =>
        matchesBackupFilters(item, typeFilter.value, statusFilter.value),
      )
      .sort((a, b) => b.created_at.localeCompare(a.created_at)),
  );

  const running = computed(() =>
    backupsStore.allBackups.some((item) => item.status === "running"),
  );

  const providerOptions = computed<SelectOption[]>(() =>
    backupsStore.providers.map((provider) => ({
      label:
        provider.kind === "s3"
          ? `${provider.name} · ${provider.bucket || provider.kind}`
          : provider.name,
      value: provider.id,
    })),
  );

  const schedules = computed(() =>
    [...backupsStore.allSchedules].sort((a, b) =>
      a.next_run_at.localeCompare(b.next_run_at),
    ),
  );

  const typeFilterOptions = computed<SelectOption[]>(() => [
    { label: t("backups.filters.allTypes"), value: "all" },
    ...(["application", "database", "service"] as ResourceType[]).map(
      (type) => ({ label: resourceTypeLabel(type), value: type }),
    ),
  ]);

  const statusFilterOptions = computed<SelectOption[]>(() => [
    { label: t("backups.filters.allStatuses"), value: "all" },
    { label: t("databases.backups.runStatus.running"), value: "running" },
    { label: t("databases.backups.runStatus.completed"), value: "completed" },
    { label: t("databases.backups.runStatus.failed"), value: "failed" },
  ]);

  function resourceOptions(): SelectOption[] {
    return SUPPORTED_RESOURCE_TYPES.map((type) => ({
      type: "group" as const,
      label: resourceTypeLabel(type),
      key: type,
      children: Object.values(resources.value)
        .filter((item) => item.type === type)
        .map((item) => ({
          label: item.name,
          value: resourceKeyOf(item.type, item.id),
        })),
    })).filter((group) => group.children.length > 0);
  }

  const resourceSelectOptions = computed<SelectOption[]>(() =>
    resourceOptions(),
  );

  function resourceFromKey(key: string): ResourceRef | null {
    const separator = key.indexOf(":");
    if (separator === -1) {
      return null;
    }
    return {
      resource_type: key.slice(0, separator) as ResourceType,
      resource_id: key.slice(separator + 1),
    };
  }

  function providerName(
    providerId: string | undefined,
    runProviderName?: string,
    providerRemoved = false,
  ): string {
    if (providerRemoved) {
      return t("backups.storage.providerRemoved");
    }
    if (!providerId) {
      return t("backups.storage.localDisk");
    }
    return (
      backupsStore.providerOf(providerId)?.name ??
      runProviderName ??
      t("backups.storage.unknownProvider")
    );
  }

  async function load(): Promise<void> {
    await Promise.allSettled([
      backupsStore.fetchAllBackups(),
      backupsStore.fetchAllSchedules(),
      isOwner.value ? backupsStore.fetchProviders() : Promise.resolve(),
      listDatabases().then((items) => {
        databases.value = items;
      }),
      listApplications().then((items) => {
        applications.value = items;
      }),
      listServices().then((items) => {
        services.value = items;
      }),
    ]);
  }

  function openBackupNow(): void {
    newResourceKey.value =
      resourceSelectOptions.value.flatMap((group) =>
        "children" in group && Array.isArray(group.children)
          ? group.children.map((child) => String(child.value))
          : [],
      )[0] ?? "";
    newProviderId.value = "";
    backupDialogOpen.value = true;
  }

  async function submitBackupNow(): Promise<void> {
    const ref = resourceFromKey(newResourceKey.value);
    if (!ref) {
      message.error(t("backups.backupNow.chooseResource"));
      return;
    }
    try {
      await backupsStore.backupNow(ref, newProviderId.value);
      message.success(t("databases.backups.runs.queued"));
      backupDialogOpen.value = false;
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  function openScheduleCreate(): void {
    scheduleResourceKey.value = "";
    scheduleCron.value = CRON_PRESETS[0];
    scheduleProviderId.value = "";
    scheduleEnabled.value = true;
    scheduleMaxRunMinutes.value = 120;
    scheduleKeepLast.value = 7;
    scheduleDialogOpen.value = true;
  }

  async function submitSchedule(): Promise<void> {
    const ref = resourceFromKey(scheduleResourceKey.value);
    if (!ref) {
      message.error(t("backups.schedule.chooseResource"));
      return;
    }
    if (!isCronPresent(scheduleCron.value)) {
      message.error(resolveValidationMessage(backupMessages.cronRequired));
      return;
    }
    if (!isKeepLastValid(scheduleKeepLast.value)) {
      message.error(resolveValidationMessage(backupMessages.keepLastInvalid));
      return;
    }
    try {
      await backupsStore.addSchedule(ref, {
        cron: scheduleCron.value.trim(),
        provider_id: scheduleProviderId.value,
        enabled: scheduleEnabled.value,
        max_run_seconds: Math.round(scheduleMaxRunMinutes.value * 60),
        keep_last: scheduleKeepLast.value ?? 7,
      });
      message.success(
        t("databases.backups.schedules.saved", { cron: scheduleCron.value.trim() }),
      );
      scheduleDialogOpen.value = false;
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  async function handleToggleSchedule(
    schedule: BackupSchedule,
    enabled: boolean,
  ): Promise<void> {
    try {
      await backupsStore.editSchedule(schedule, schedule.id, {
        cron: schedule.cron,
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

  async function handleDeleteSchedule(schedule: BackupSchedule): Promise<void> {
    try {
      await backupsStore.removeSchedule(schedule, schedule.id);
      message.success(t("databases.backups.schedules.deleted"));
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

  async function handleRetryCleanup(backup: BackupRun): Promise<void> {
    try {
      await backupsStore.retryCleanup(backup);
      message.success(t("backups.runs.cleanupDone"));
    } catch (error) {
      message.error(describeBackupError(error));
    }
  }

  async function handleDiscardCleanup(backup: BackupRun): Promise<void> {
    try {
      await backupsStore.discardCleanup(backup);
      message.success(t("backups.runs.cleanupDiscarded"));
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
      await backupsStore.restore(restoreCandidate.value.id);
      message.success(t("databases.backups.restores.queued"));
      restoreOpen.value = false;
      restoreCandidate.value = null;
      await backupsStore.fetchAllBackups();
      syncPolling();
    } catch (error) {
      message.error(describeBackupError(error));
    } finally {
      restoring.value = false;
    }
  }

  async function refresh(): Promise<void> {
    await load();
  }

  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  function syncPolling(): void {
    stopPolling();
    if (running.value) {
      pollTimer = setInterval(() => {
        void backupsStore.fetchAllBackups();
      }, 5_000);
    }
  }

  onUnmounted(stopPolling);

  return {
    backupsStore,
    isOwner,
    typeFilter,
    statusFilter,
    typeFilterOptions,
    statusFilterOptions,
    providerOptions,
    filteredBackups,
    schedules,
    resolveResource,
    providerName,
    load,
    refresh,
    backupDialogOpen,
    newResourceKey,
    newProviderId,
    resourceSelectOptions,
    openBackupNow,
    submitBackupNow,
    scheduleDialogOpen,
    scheduleResourceKey,
    scheduleCron,
    scheduleProviderId,
    scheduleEnabled,
    scheduleMaxRunMinutes,
    scheduleKeepLast,
    openScheduleCreate,
    submitSchedule,
    handleToggleSchedule,
    handleDeleteSchedule,
    handleDeleteBackup,
    handleRetryCleanup,
    handleDiscardCleanup,
    restoreOpen,
    restoreCandidate,
    restoring,
    openRestore,
    handleRestoreConfirm,
    resourceTypeLabel,
  };
}

export type BackupsOverview = ReturnType<typeof useBackupsOverview>;
