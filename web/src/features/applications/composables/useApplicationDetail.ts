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
import type { Preview } from "@/features/applications/api/previews";
import {
  describePreviewError,
  isFeatureDisabled as isPreviewsDisabled,
  listPreviews,
} from "@/features/applications/api/previews";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import {
  getEnvironmentVariables,
  getProjectVariables,
} from "@/features/projects/api/variables";
import type { InheritedVariable } from "@/features/projects/schemas/variables";
import { resolveEnvironmentScope } from "@/features/projects/utils/canonicalRoutes";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { useTeamsStore } from "@/features/teams";
import { useServersStore } from "@/features/servers";
import { activeLocale, i18n } from "@/shared/i18n";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import { pipelineStepsFor } from "@/features/applications/utils/deployPipeline";
import { createRequestGeneration } from "@/shared/utils/requestGeneration";

/**
 * State, data loading and mutations behind the application detail page.
 * The page itself only handles route params, layout and tab composition.
 */
export function useApplicationDetail() {
  const route = useRoute();
  const router = useRouter();
  const message = useMessage();
  const appsStore = useApplicationsStore();
  const projectsStore = useProjectsStore();
  const teamsStore = useTeamsStore();
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
  /**
   * envErrorRaw keeps the failure behind the env banner; envError derives its
   * display text in the current locale so a language switch refreshes the
   * banner without clearing the draft or refetching.
   */
  const envErrorRaw = ref<unknown>(null);
  const envError = computed<string | null>(() => {
    if (envErrorRaw.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeApplicationError(envErrorRaw.value);
  });
  // `envLoadedFor` names the application whose environment actually loaded. Save
  // is only enabled while it matches the current application, so an empty draft
  // from a failed read (or a draft left over from another application) can never
  // be written over unknown data.
  const envLoadedFor = ref<string>("");
  const storagesDraft = ref<StorageMapping[]>([]);
  const storagesLoading = ref(false);
  /** storagesErrorRaw/storagesError mirror the env pair for volumes. */
  const storagesErrorRaw = ref<unknown>(null);
  const storagesError = computed<string | null>(() => {
    if (storagesErrorRaw.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeApplicationError(storagesErrorRaw.value);
  });
  const storagesLoadedFor = ref<string>("");

  // Inherited shared variables (PE-6): the project's and the environment's
  // masked sets, shown read-only above the application editor with their
  // origin. Context only — a failed read renders as no section, never an
  // error over the application editor the user came to change.
  const inheritedVars = ref<InheritedVariable[]>([]);
  const inheritedLoading = ref(false);
  const inheritedReady = ref(false);

  // Invalidates in-flight config reads when the route's application changes, so
  // a late response cannot overwrite the new application's draft.
  const draftGeneration = createRequestGeneration();

  // Previews (FE-8.1). `previewsLoaded` is only set by a successful read, so an
  // unavailable list can never render as a confirmed-empty one: a failure keeps
  // the previously loaded rows and shows an explicit error with a retry, and a
  // feature-flag 404 hides the whole tab instead of erroring.
  const previews = ref<Preview[]>([]);
  const previewsLoading = ref(false);
  const previewsLoaded = ref(false);
  /** previewsErrorRaw/previewsError mirror the env pair for previews. */
  const previewsErrorRaw = ref<unknown>(null);
  const previewsError = computed<string | null>(() => {
    if (previewsErrorRaw.value === null) {
      return null;
    }
    void activeLocale.value;
    return describePreviewError(previewsErrorRaw.value);
  });
  const previewsAvailable = ref(true);

  /** isNarrow stacks the two-column descriptions on small screens. */
  const isNarrow = useMediaQuery("(max-width: 640px)");

  /** descColumns renders descriptions in one column below 640px. */
  const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));

  const deployments = computed<Deployment[]>(() => appsStore.deploymentsOf(appId.value));

  const application = computed<Application | null>(() =>
    appsStore.applicationOf(appId.value),
  );

  /** displayName prefers the stored name, falling back to the id head. */
  const displayName = computed<string>(() => application.value?.name ?? shortId.value);

  /** hasContainer reports whether any deployment started a container. */
  const hasContainer = computed<boolean>(() =>
    deployments.value.some((item) => item.container_id !== ""),
  );

  /**
   * tr resolves one applications message in the current locale. Reading
   * activeLocale pins the calling computed to the language switch.
   */
  function tr(key: string, params?: Record<string, string | number>): string {
    void activeLocale.value;
    return String(i18n.global.t(key, params ?? {}));
  }

  /**
   * controlHint explains why stop/start are unavailable, if they are: an
   * in-flight deployment owns the container right now, or no deployment ever
   * started one (the backend would answer 404).
   */
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

  /**
   * containerStopped tracks a stop/start the deployment row cannot express:
   * stopping leaves the deployment state "running" (the release still describes
   * the container), so the header tag and the Stop/Start buttons read this local
   * truth instead of `latest.state` alone (C4-19).
   */
  const containerStopped = ref(false);

  /**
   * stoppedForContainer names the container the local stop override applies to.
   * stopApp → refreshDeployments must not clear the override when the refresh
   * merely re-keys the row for the same container.
   */
  const stoppedForContainer = ref<string>("");

  /** containerIsRunning is the live view of the newest deployment's container. */
  const containerIsRunning = computed<boolean>(
    () => latest.value?.state === "running" && !containerStopped.value,
  );

  /** shortId renders the head of the application UUID for the header. */
  const shortId = computed<string>(() => appId.value.slice(0, 8));

  /** initials derives a two-letter avatar from the application id. */
  const initials = computed<string>(() => (shortId.value.slice(0, 2) || "AP").toUpperCase());

  /** runningDeployments lists terminal running rows eligible for rollback. */
  const runningDeployments = computed<Deployment[]>(() =>
    deployments.value.filter((item) => item.state === "running"),
  );

  /** runtimeDeployment streams the newest running deployment's container. */
  const runtimeDeployment = computed<Deployment | null>(
    () =>
      deployments.value.find(
        (item) => item.state === "running" && item.container_id !== "",
      ) ?? null,
  );

  /** logTarget resolves the deployment selected in the Logs tab. */
  const logTarget = computed<Deployment | null>(
    () => deployments.value.find((item) => item.id === logDeploymentId.value) ?? active.value ?? latest.value,
  );

  /** effectiveLogServerId streams the explicit pick, else the app node, else the first node. */
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

  /**
   * Logs tab picks stay explicit-only: an empty id means "follow the
   * default", so the tab keeps tracking the active/latest deployment and the
   * application node without default-writing watchers (no ordering bugs, no
   * stale pins, warm caches and late arrivals just work).
   */
  const defaultLogDeploymentId = computed<string>(
    () => active.value?.id ?? latest.value?.id ?? "",
  );

  /** displayedLogDeploymentId shows the explicit pick while listed, else the default. */
  const displayedLogDeploymentId = computed<string>(() =>
    logDeploymentId.value !== "" &&
    deployments.value.some((item) => item.id === logDeploymentId.value)
      ? logDeploymentId.value
      : defaultLogDeploymentId.value,
  );

  /** defaultLogServerId prefers the application node when known, else the first node. */
  const defaultLogServerId = computed<string>(() => {
    const servers = serversStore.servers;
    const appServer = application.value?.server_id ?? "";
    if (appServer !== "" && servers.some((server) => server.id === appServer)) {
      return appServer;
    }
    return servers[0]?.id ?? "";
  });

  /** displayedLogServerId shows the explicit pick while listed, else the default. */
  const displayedLogServerId = computed<string>(() =>
    logServerId.value !== "" &&
    serversStore.servers.some((server) => server.id === logServerId.value)
      ? logServerId.value
      : defaultLogServerId.value,
  );

  /** pipelineSteps maps the latest deployment onto done/active/todo/failed. */
  const pipelineSteps = computed(() => pipelineStepsFor(latest.value));

  /** fetchAll loads the application, its history, config and node list. */
  async function fetchAll(): Promise<void> {
    if (!appId.value) {
      return;
    }
    let application: Application | null = null;
    try {
      application = await appsStore.fetchApplication(appId.value);
    } catch {
      // The store already exposes the error; the alert renders it.
    }
    if (application) {
      // The row ids are the authority: a wrong project/environment in the
      // URL replaces it with the canonical nested URL instead of rendering
      // silently. The route watcher reloads from there.
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
      // The store already exposes the error; the alert renders it.
    }
    void loadEnv();
    void loadStorages();
    void loadInherited();
    void loadPreviews();
    void serversStore.fetchServers().catch(() => undefined);
  }

  /** loadEnv refreshes the environment draft shown in the editor. */
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
        return; // superseded by an application switch
      }
      envDraft.value = [...env];
      envLoadedFor.value = target;
    } catch (error) {
      if (!draftGeneration.isCurrent(token) || target !== appId.value) {
        return;
      }
      // Never present a failed read as an empty collection: keep the draft in an
      // error state and clear envLoadedFor so Save stays disabled until a
      // successful read. An unknown server state is never overwritten.
      envErrorRaw.value = error;
      envLoadedFor.value = "";
    } finally {
      if (draftGeneration.isCurrent(token) && target === appId.value) {
        envLoading.value = false;
      }
    }
  }

  /** loadInherited refreshes the read-only project/environment context rows. */
  async function loadInherited(): Promise<void> {
    const target = appId.value;
    const scope = application.value;
    if (target === "" || !scope) {
      return;
    }
    const teamId = teamsStore.activeTeamId;
    const projectId = scope.project_id;
    const environmentId = scope.environment_id;
    inheritedLoading.value = true;
    try {
      // Either scope may fail independently (a 404 on one must not drop
      // the other scope's rows): render whichever half succeeded.
      const [projectResult, environmentResult] = await Promise.allSettled([
        getProjectVariables(teamId, projectId),
        getEnvironmentVariables(teamId, environmentId),
      ]);
      if (target !== appId.value) {
        return; // superseded by an application switch
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
      // Context only: a failed read renders as no section, and the
      // application editor below keeps working.
      inheritedVars.value = [];
      inheritedReady.value = false;
    } finally {
      if (target === appId.value) {
        inheritedLoading.value = false;
      }
    }
  }

  /** loadStorages refreshes the volume draft shown in the editor. */
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

  /**
   * loadPreviews refreshes the application's preview bindings. A feature-flag
   * 404 hides the tab; any other failure keeps the rows already shown and
   * surfaces an explicit error with a retry (an unavailable list is never
   * rendered as an empty one).
   */
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

  /** handleSaveEnv replaces the whole environment collection. */
  async function handleSaveEnv(): Promise<void> {
    const target = appId.value;
    // Refuse to write a draft that does not belong to the current application:
    // an empty or stale draft must never replace unknown server-side data.
    if (target === "" || envLoadedFor.value !== target) {
      return;
    }
    const token = draftGeneration.current();
    envErrorRaw.value = null;
    try {
      const saved = await appsStore.saveEnv(target, envDraft.value);
      // Guard by generation as well as id: A→B→A must not let A's old save
      // response overwrite the draft B's navigation reloaded.
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

  /** handleSaveStorages replaces the whole storage collection. */
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

  /** handleDeploy queues a redeploy of the current revision. */
  async function handleDeploy(): Promise<void> {
    try {
      await appsStore.deploy(appId.value);
      message.success(tr("applications.detail.deployQueued"));
    } catch (error) {
      message.error(describeApplicationError(error));
    }
  }

  /** handleRollback queues a rollback to the selected running deployment. */
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

  /** handleStop stops the container of the newest deployment. */
  async function handleStop(): Promise<void> {
    try {
      await appsStore.stopApp(appId.value);
      // A stop leaves the deployment row "running"; reflect the live container
      // so the tag flips and Stop disables (C4-19).
      containerStopped.value = true;
      stoppedForContainer.value = latest.value?.container_id ?? "";
      message.success(tr("applications.detail.stopSent"));
    } catch (error) {
      message.error(describeApplicationError(error, "stop"));
    }
  }

  /** handleStart restarts the container of the newest deployment. */
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

  /** openRollback preselects the previous release and opens the dialog. */
  function openRollback(): void {
    const candidates = runningDeployments.value;
    // Default to the previous successful release; fall back to the only one.
    rollbackTarget.value = candidates[1]?.id ?? candidates[0]?.id ?? "";
    rollbackOpen.value = true;
  }

  /** showLogsFor jumps to the Logs tab with a deployment preselected. */
  function showLogsFor(deploymentId: string): void {
    logDeploymentId.value = deploymentId;
    activeTab.value = "logs";
  }

  /** openRollbackFor preselects a deployment and opens the dialog. */
  function openRollbackFor(deploymentId: string): void {
    rollbackTarget.value = deploymentId;
    rollbackOpen.value = true;
  }

  /** canWrite follows the contract's roles: viewers read, members write. */
  const canWrite = computed<boolean>(() => projectsStore.canWrite);

  const moveSaving = ref(false);
  /** moveErrorRaw/moveError mirror the env pair for location writes. */
  const moveErrorRaw = ref<unknown>(null);
  const moveError = computed<string | null>(() => {
    if (moveErrorRaw.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeApplicationError(moveErrorRaw.value);
  });

  /**
   * handleMove applies the location settings (move environment, change
   * node). Only changed fields ride the PUT; a move retargets the nested
   * route to the new environment. The contract's 409 refusals (open
   * previews, in-flight deploy, name collision) render inline through
   * moveError. The navigation is identity-guarded: leaving the resource
   * mid-request never yanks the user back to it.
   */
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
        return; // the route moved on while the write was in flight
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

  /**
   * handleDelete removes the application and returns to its environment. The
   * store drops the cached row, the projects refresh converges the counts,
   * and the environment page reloads its resource list on mount, so the row
   * disappears. The navigation is identity-guarded like the move above.
   */
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

  /**
   * refreshProjectCounts invalidates the projects store after a move, so
   * project/environment counts converge without relying on a remount.
   */
  async function refreshProjectCounts(projectIds: string[]): Promise<void> {
    try {
      await projectsStore.fetchProjects();
    } catch {
      // The store already exposes the error; counts converge on next load.
    }
    const detail = projectsStore.detail;
    if (detail && projectIds.includes(detail.id)) {
      try {
        await projectsStore.fetchDetail(detail.id);
      } catch {
        // Same as above; the detail alert renders it.
      }
    }
  }

  // The detail route is reused when navigating between applications, and a
  // move keeps the id while the environment changes: reload on either.
  watch([appId, () => String(route.params.environmentId ?? "")], () => {
    // Invalidate any in-flight config read for the previous application.
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
    previews.value = [];
    previewsLoaded.value = false;
    previewsErrorRaw.value = null;
    previewsAvailable.value = true;
    appsStore.stopAllPolling();
    void fetchAll();
  });

  // A new deployment's container supersedes any local stop/start override —
  // but a refresh that merely re-keys the row for the same stopped container
  // keeps it.
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

  onMounted(() => {
    // A progress-card "View logs" link lands here with ?logs=<deploymentId>
    // (JUS-91): jump straight to the Logs tab with that deployment selected.
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
    initials,
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
    loadInherited,
    loadPreviews,
  };
}

/** DetailState is the shared shape passed from the page to its tab panels. */
export type DetailState = ReturnType<typeof useApplicationDetail>;
