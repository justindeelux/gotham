import { useMessage } from "naive-ui";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { describeApplicationError, isActiveDeployment } from "@/features/applications/api/applications";
import type {
  Application,
  Deployment,
  EnvVar,
  StorageMapping,
  UpdateApplicationInput,
} from "@/features/applications/api/applications";
import { listVolumes } from "@/features/applications/api/applications";
import type { VolumeListing } from "@/features/applications/schemas/volumes";
import type { Preview } from "@/features/applications/api/previews";
import {
  describePreviewError,
  isFeatureDisabled as isPreviewsDisabled,
  listPreviews,
} from "@/features/applications/api/previews";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import { applicationBrand } from "@/features/add-resource";
import type { BrandMark } from "@/features/add-resource";
import {
  getEnvironmentVariables,
  getProjectVariables,
} from "@/features/projects/api/variables";
import type { InheritedVariable } from "@/features/projects/schemas/variables";
import { resolveEnvironmentScope } from "@/features/projects/utils/canonicalRoutes";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { useServersStore } from "@/features/servers";
import { activeLocale, i18n } from "@/shared/i18n";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import { pipelineStepsFor } from "@/features/applications/utils/deployPipeline";
import { createRequestGeneration } from "@/shared/utils/requestGeneration";

export function useApplicationDetail() {
  const route = useRoute();
  const router = useRouter();
  const message = useMessage();
  const appsStore = useApplicationsStore();
  const projectsStore = useProjectsStore();
  const serversStore = useServersStore();

  const appId = computed<string>(() => String(route.params.id ?? ""));
  const activeTab = ref("overview");
  const logDeploymentId = ref<string>("");
  const logServerId = ref<string>("");
  const rollbackOpen = ref(false);
  const rollbackTarget = ref<string>("");
  const rollingBack = ref(false);
  const deleteOpen = ref(false);
  const deleting = ref(false);
  const envDraft = ref<EnvVar[]>([]);
  const envLoading = ref(false);
  const envErrorRaw = ref<unknown>(null);
  const envError = computed<string | null>(() => {
    if (envErrorRaw.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeApplicationError(envErrorRaw.value);
  });
  const envLoadedFor = ref<string>("");
  const storagesDraft = ref<StorageMapping[]>([]);
  const storagesLoading = ref(false);
  const storagesErrorRaw = ref<unknown>(null);
  const storagesError = computed<string | null>(() => {
    if (storagesErrorRaw.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeApplicationError(storagesErrorRaw.value);
  });
  const storagesLoadedFor = ref<string>("");
  const volumes = ref<VolumeListing | null>(null);
  const volumesLoading = ref(false);
  const volumesErrorRaw = ref<unknown>(null);
  const volumesError = computed<string | null>(() => {
    if (volumesErrorRaw.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeApplicationError(volumesErrorRaw.value);
  });
  const volumesLoadedFor = ref<string>("");

  const inheritedVars = ref<InheritedVariable[]>([]);
  const inheritedLoading = ref(false);
  const inheritedReady = ref(false);

  const draftGeneration = createRequestGeneration();

  const previews = ref<Preview[]>([]);
  const previewsLoading = ref(false);
  const previewsLoaded = ref(false);
  const previewsErrorRaw = ref<unknown>(null);
  const previewsError = computed<string | null>(() => {
    if (previewsErrorRaw.value === null) {
      return null;
    }
    void activeLocale.value;
    return describePreviewError(previewsErrorRaw.value);
  });
  const previewsAvailable = ref(true);

  const isNarrow = useMediaQuery("(max-width: 640px)");

  const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));

  const deployments = computed<Deployment[]>(() => appsStore.deploymentsOf(appId.value));

  const application = computed<Application | null>(() =>
    appsStore.applicationOf(appId.value),
  );

  const displayName = computed<string>(() => application.value?.name ?? shortId.value);

  const hasContainer = computed<boolean>(() =>
    deployments.value.some((item) => item.container_id !== ""),
  );

  function tr(key: string, params?: Record<string, string | number>): string {
    void activeLocale.value;
    return String(i18n.global.t(key, params ?? {}));
  }

  const controlHint = computed<string | null>(() => {
    if (active.value !== null) {
      return tr("applications.detail.controlInProgress");
    }
    if (!hasContainer.value) {
      return tr("applications.detail.controlNoContainer");
    }
    return null;
  });

  const latest = computed<Deployment | null>(() => appsStore.latestDeployment(appId.value));

  const active = computed<Deployment | null>(() => appsStore.activeDeployment(appId.value));

  const containerStopped = ref(false);

  const stoppedForContainer = ref<string>("");

  const containerIsRunning = computed<boolean>(
    () => latest.value?.state === "running" && !containerStopped.value,
  );

  const shortId = computed<string>(() => appId.value.slice(0, 8));

  const brand = computed<BrandMark>(() =>
    applicationBrand(
      application.value?.source_type ?? "",
      application.value?.provider ?? "",
      displayName.value,
    ),
  );

  const runningDeployments = computed<Deployment[]>(() =>
    deployments.value.filter((item) => item.state === "running"),
  );

  const runtimeDeployment = computed<Deployment | null>(
    () =>
      deployments.value.find(
        (item) => item.state === "running" && item.container_id !== "",
      ) ?? null,
  );

  const logTarget = computed<Deployment | null>(
    () => deployments.value.find((item) => item.id === logDeploymentId.value) ?? active.value ?? latest.value,
  );

  const effectiveLogServerId = computed<string>(() => {
    const servers = serversStore.servers;
    if (
      logServerId.value !== "" &&
      servers.some((server) => server.id === logServerId.value)
    ) {
      return logServerId.value;
    }
    const appServer = application.value?.server_id ?? "";
    if (appServer !== "") {
      return appServer;
    }
    return servers[0]?.id ?? "";
  });

  const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
    serversStore.servers.map((server) => ({
      label: `${server.name} · ${server.ip}`,
      value: server.id,
    })),
  );

  const deploymentOptions = computed<Array<{ label: string; value: string }>>(() =>
    deployments.value.map((item) => ({
      label: `${item.id.slice(0, 8)} · ${item.kind} · ${tr(`applications.status.${item.state}`)}`,
      value: item.id,
    })),
  );

  const defaultLogDeploymentId = computed<string>(
    () => active.value?.id ?? latest.value?.id ?? "",
  );

  const displayedLogDeploymentId = computed<string>(() =>
    logDeploymentId.value !== "" &&
    deployments.value.some((item) => item.id === logDeploymentId.value)
      ? logDeploymentId.value
      : defaultLogDeploymentId.value,
  );

  const defaultLogServerId = computed<string>(() => {
    const servers = serversStore.servers;
    const appServer = application.value?.server_id ?? "";
    if (appServer !== "" && servers.some((server) => server.id === appServer)) {
      return appServer;
    }
    return servers[0]?.id ?? "";
  });

  const displayedLogServerId = computed<string>(() =>
    logServerId.value !== "" &&
    serversStore.servers.some((server) => server.id === logServerId.value)
      ? logServerId.value
      : defaultLogServerId.value,
  );

  const pipelineSteps = computed(() => pipelineStepsFor(latest.value));

  async function fetchAll(): Promise<void> {
    if (!appId.value) {
      return;
    }
    let application: Application | null = null;
    try {
      application = await appsStore.fetchApplication(appId.value);
    } catch {
    }
    if (application) {
      const canonical = resolveEnvironmentScope(
        { projectId: application.project_id, environmentId: application.environment_id },
        {
          projectId: String(route.params.projectId ?? ""),
          environmentId: String(route.params.environmentId ?? ""),
        },
      );
      if (canonical !== null) {
        await router.replace({
          name: "application-detail",
          params: { projectId: canonical.projectId, environmentId: canonical.environmentId, id: appId.value },
        });
        return;
      }
    }
    try {
      await appsStore.fetchDeployments(appId.value);
    } catch {
    }
    void loadEnv();
    void loadStorages();
    void loadVolumes();
    void loadInherited();
    void loadPreviews();
    void serversStore.fetchServers().catch(() => undefined);
  }

  async function loadEnv(): Promise<void> {
    const target = appId.value;
    if (target === "") {
      return;
    }
    const token = draftGeneration.current();
    envLoading.value = true;
    envErrorRaw.value = null;
    try {
      const env = await appsStore.fetchEnv(target);
      if (!draftGeneration.isCurrent(token) || target !== appId.value) {
        return;
      }
      envDraft.value = [...env];
      envLoadedFor.value = target;
    } catch (error) {
      if (!draftGeneration.isCurrent(token) || target !== appId.value) {
        return;
      }
      envErrorRaw.value = error;
      envLoadedFor.value = "";
    } finally {
      if (draftGeneration.isCurrent(token) && target === appId.value) {
        envLoading.value = false;
      }
    }
  }

  async function loadInherited(): Promise<void> {
    const target = appId.value;
    const scope = application.value;
    if (target === "" || !scope) {
      return;
    }
    const projectId = scope.project_id;
    const environmentId = scope.environment_id;
    inheritedLoading.value = true;
    try {
      const [projectResult, environmentResult] = await Promise.allSettled([
        getProjectVariables(projectId),
        getEnvironmentVariables(environmentId),
      ]);
      if (target !== appId.value) {
        return;
      }
      const projectVars =
        projectResult.status === "fulfilled" ? projectResult.value : [];
      const environmentVars =
        environmentResult.status === "fulfilled" ? environmentResult.value : [];
      inheritedVars.value = [
        ...projectVars.map((row) => ({ ...row, origin: "project" as const })),
        ...environmentVars.map((row) => ({ ...row, origin: "environment" as const })),
      ];
      inheritedReady.value =
        projectResult.status === "fulfilled" ||
        environmentResult.status === "fulfilled";
    } catch {
      if (target !== appId.value) {
        return;
      }
      inheritedVars.value = [];
      inheritedReady.value = false;
    } finally {
      if (target === appId.value) {
        inheritedLoading.value = false;
      }
    }
  }

  async function loadStorages(): Promise<void> {
    const target = appId.value;
    if (target === "") {
      return;
    }
    const token = draftGeneration.current();
    storagesLoading.value = true;
    storagesErrorRaw.value = null;
    try {
      const storages = await appsStore.fetchStorages(target);
      if (!draftGeneration.isCurrent(token) || target !== appId.value) {
        return;
      }
      storagesDraft.value = [...storages];
      storagesLoadedFor.value = target;
    } catch (error) {
      if (!draftGeneration.isCurrent(token) || target !== appId.value) {
        return;
      }
      storagesErrorRaw.value = error;
      storagesLoadedFor.value = "";
    } finally {
      if (draftGeneration.isCurrent(token) && target === appId.value) {
        storagesLoading.value = false;
      }
    }
  }

  async function loadVolumes(): Promise<void> {
    const target = appId.value;
    if (target === "") {
      return;
    }
    const token = draftGeneration.current();
    volumesLoading.value = true;
    volumesErrorRaw.value = null;
    try {
      const listing = await listVolumes(target);
      if (!draftGeneration.isCurrent(token) || target !== appId.value) {
        return;
      }
      volumes.value = listing;
      volumesLoadedFor.value = target;
    } catch (error) {
      if (!draftGeneration.isCurrent(token) || target !== appId.value) {
        return;
      }
      volumesErrorRaw.value = error;
      volumesLoadedFor.value = "";
    } finally {
      if (draftGeneration.isCurrent(token) && target === appId.value) {
        volumesLoading.value = false;
      }
    }
  }

  async function loadPreviews(): Promise<void> {
    if (!appId.value) {
      return;
    }
    previewsLoading.value = true;
    previewsErrorRaw.value = null;
    try {
      previews.value = await listPreviews(appId.value);
      previewsLoaded.value = true;
      previewsAvailable.value = true;
    } catch (error) {
      if (isPreviewsDisabled(error)) {
        previews.value = [];
        previewsLoaded.value = false;
        previewsAvailable.value = false;
        return;
      }
      previewsErrorRaw.value = error;
    } finally {
      previewsLoading.value = false;
    }
  }

  async function handleSaveEnv(): Promise<void> {
    const target = appId.value;
    if (target === "" || envLoadedFor.value !== target) {
      return;
    }
    const token = draftGeneration.current();
    envErrorRaw.value = null;
    try {
      const saved = await appsStore.saveEnv(target, envDraft.value);
      if (target !== appId.value || !draftGeneration.isCurrent(token)) {
        return;
      }
      envDraft.value = [...saved];
      message.success(tr("applications.detail.envSaved"));
    } catch (error) {
      if (target !== appId.value || !draftGeneration.isCurrent(token)) {
        return;
      }
      envErrorRaw.value = error;
    }
  }

  async function handleSaveStorages(): Promise<void> {
    const target = appId.value;
    if (target === "" || storagesLoadedFor.value !== target) {
      return;
    }
    const token = draftGeneration.current();
    storagesErrorRaw.value = null;
    try {
      const saved = await appsStore.saveStorages(target, storagesDraft.value);
      if (target !== appId.value || !draftGeneration.isCurrent(token)) {
        return;
      }
      storagesDraft.value = [...saved];
      message.success(tr("applications.detail.volumesSaved"));
    } catch (error) {
      if (target !== appId.value || !draftGeneration.isCurrent(token)) {
        return;
      }
      storagesErrorRaw.value = error;
    }
  }

  async function handleDeploy(): Promise<void> {
    try {
      await appsStore.deploy(appId.value);
      message.success(tr("applications.detail.deployQueued"));
    } catch (error) {
      message.error(describeApplicationError(error));
    }
  }

  async function handleRollback(): Promise<void> {
    rollingBack.value = true;
    try {
      await appsStore.rollback(appId.value, rollbackTarget.value || undefined);
      message.success(tr("applications.detail.rollbackQueued"));
      rollbackOpen.value = false;
    } catch (error) {
      message.error(describeApplicationError(error));
    } finally {
      rollingBack.value = false;
    }
  }

  async function handleStop(): Promise<void> {
    try {
      await appsStore.stopApp(appId.value);
      containerStopped.value = true;
      stoppedForContainer.value = latest.value?.container_id ?? "";
      message.success(tr("applications.detail.stopSent"));
    } catch (error) {
      message.error(describeApplicationError(error, "stop"));
    }
  }

  async function handleStart(): Promise<void> {
    try {
      await appsStore.startApp(appId.value);
      containerStopped.value = false;
      stoppedForContainer.value = "";
      message.success(tr("applications.detail.startSent"));
    } catch (error) {
      message.error(describeApplicationError(error, "start"));
    }
  }

  function openRollback(): void {
    const candidates = runningDeployments.value;
    rollbackTarget.value = candidates[1]?.id ?? candidates[0]?.id ?? "";
    rollbackOpen.value = true;
  }

  function showLogsFor(deploymentId: string): void {
    logDeploymentId.value = deploymentId;
    activeTab.value = "logs";
  }

  const DETAIL_TABS = new Set([
    "overview",
    "deployments",
    "logs",
    "env",
    "terminal",
    "storage",
    "domains",
    "previews",
    "settings",
  ]);

  function routeTab(): string | null {
    const tab = route.query.tab;
    return typeof tab === "string" && DETAIL_TABS.has(tab) ? tab : null;
  }

  function applyRouteTab(app: Application | null): void {
    if (app === null) {
      return;
    }
    const tab = routeTab();
    if (tab === null) {
      return;
    }
    activeTab.value = tab;
  }

  function openRollbackFor(deploymentId: string): void {
    rollbackTarget.value = deploymentId;
    rollbackOpen.value = true;
  }

  const canWrite = computed<boolean>(() => projectsStore.canWrite);

  const moveSaving = ref(false);
  const moveErrorRaw = ref<unknown>(null);
  const moveError = computed<string | null>(() => {
    if (moveErrorRaw.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeApplicationError(moveErrorRaw.value);
  });

  async function handleMove(scope: {
    projectId: string;
    environmentId: string;
    serverId: string;
  }): Promise<void> {
    const current = application.value;
    if (!current) {
      return;
    }
    const input: UpdateApplicationInput = {};
    if (scope.environmentId !== current.environment_id) {
      input.environment_id = scope.environmentId;
    }
    if (scope.serverId !== (current.server_id ?? "")) {
      input.server_id = scope.serverId;
    }
    if (Object.keys(input).length === 0) {
      return;
    }
    const targetId = appId.value;
    moveSaving.value = true;
    moveErrorRaw.value = null;
    try {
      const updated = await appsStore.update(targetId, input);
      if (targetId !== appId.value) {
        return;
      }
      message.success(tr("applications.detail.locationSaved"));
      await refreshProjectCounts([current.project_id, updated.project_id]);
      if (targetId !== appId.value) {
        return;
      }
      if (input.environment_id) {
        await router.push({
          name: "application-detail",
          params: {
            projectId: updated.project_id,
            environmentId: updated.environment_id,
            id: updated.id,
          },
        });
      }
    } catch (error) {
      if (targetId === appId.value) {
        moveErrorRaw.value = error;
      }
    } finally {
      if (targetId === appId.value) {
        moveSaving.value = false;
      }
    }
  }

  async function handleDelete(): Promise<void> {
    const current = application.value;
    const targetId = appId.value;
    if (!current || targetId === "") {
      return;
    }
    deleting.value = true;
    try {
      await appsStore.remove(targetId);
      message.success(tr("applications.detail.deleted", { name: current.name }));
      deleteOpen.value = false;
      await refreshProjectCounts([current.project_id]);
      if (targetId !== appId.value) {
        return;
      }
      await router.push({
        name: "environment-detail",
        params: { projectId: current.project_id, environmentId: current.environment_id },
      });
    } catch (error) {
      if (targetId === appId.value) {
        message.error(describeApplicationError(error));
      }
    } finally {
      if (targetId === appId.value) {
        deleting.value = false;
      }
    }
  }

  async function refreshProjectCounts(projectIds: string[]): Promise<void> {
    try {
      await projectsStore.fetchProjects();
    } catch {
    }
    const detail = projectsStore.detail;
    if (detail && projectIds.includes(detail.id)) {
      try {
        await projectsStore.fetchDetail(detail.id);
      } catch {
      }
    }
  }

  watch([appId, () => String(route.params.environmentId ?? "")], () => {
    draftGeneration.bump();
    activeTab.value = "overview";
    containerStopped.value = false;
    stoppedForContainer.value = "";
    logDeploymentId.value = "";
    logServerId.value = "";
    moveSaving.value = false;
    moveErrorRaw.value = null;
    deleteOpen.value = false;
    deleting.value = false;
    envDraft.value = [];
    envErrorRaw.value = null;
    envLoadedFor.value = "";
    envLoading.value = false;
    inheritedVars.value = [];
    inheritedLoading.value = false;
    inheritedReady.value = false;
    storagesDraft.value = [];
    storagesErrorRaw.value = null;
    storagesLoadedFor.value = "";
    storagesLoading.value = false;
    volumes.value = null;
    volumesErrorRaw.value = null;
    volumesLoadedFor.value = "";
    volumesLoading.value = false;
    previews.value = [];
    previewsLoaded.value = false;
    previewsErrorRaw.value = null;
    previewsAvailable.value = true;
    appsStore.stopAllPolling();
    void fetchAll();
  });

  watch(
    () => latest.value?.id,
    () => {
      const row = latest.value;
      if (
        row &&
        !isActiveDeployment(row) &&
        stoppedForContainer.value !== "" &&
        row.container_id !== "" &&
        row.container_id === stoppedForContainer.value
      ) {
        return;
      }
      containerStopped.value = false;
    },
  );

  watch(
    [() => application.value?.id ?? "", () => route.query.tab],
    () => {
      applyRouteTab(application.value);
    },
    { immediate: true },
  );

  onMounted(() => {
    void fetchAll().then(() => {
      const logs = route.query.logs;
      if (typeof logs === "string" && logs !== "") {
        showLogsFor(logs);
      }
    });
  });

  onUnmounted(() => {
    appsStore.stopAllPolling();
  });

  return {
    appId,
    activeTab,
    application,
    displayName,
    shortId,
    brand,
    descColumns,
    deployments,
    latest,
    active,
    runningDeployments,
    runtimeDeployment,
    controlHint,
    containerStopped,
    containerIsRunning,
    pipelineSteps,
    logDeploymentId,
    logServerId,
    displayedLogDeploymentId,
    displayedLogServerId,
    logTarget,
    effectiveLogServerId,
    serverOptions,
    deploymentOptions,
    rollbackOpen,
    rollbackTarget,
    rollingBack,
    deleteOpen,
    deleting,
    envDraft,
    envLoading,
    envError,
    envLoadedFor,
    inheritedVars,
    inheritedLoading,
    inheritedReady,
    storagesDraft,
    storagesLoading,
    storagesError,
    storagesLoadedFor,
    volumes,
    volumesLoading,
    volumesError,
    volumesLoadedFor,
    previews,
    previewsLoading,
    previewsLoaded,
    previewsError,
    previewsAvailable,
    appsStore,
    canWrite,
    moveSaving,
    moveError,
    handleMove,
    handleSaveEnv,
    handleSaveStorages,
    handleDeploy,
    handleRollback,
    handleDelete,
    handleStop,
    handleStart,
    openRollback,
    showLogsFor,
    openRollbackFor,
    loadEnv,
    loadStorages,
    loadVolumes,
    loadInherited,
    loadPreviews,
  };
}

export type DetailState = ReturnType<typeof useApplicationDetail>;
