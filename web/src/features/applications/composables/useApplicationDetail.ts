import { useMessage } from "naive-ui";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { describeApplicationError, isActiveDeployment } from "@/features/applications/api/applications";
import type {
  Application,
  Deployment,
  EnvVar,
  StorageMapping,
} from "@/features/applications/api/applications";
import type { Preview } from "@/features/applications/api/previews";
import {
  describePreviewError,
  isFeatureDisabled as isPreviewsDisabled,
  listPreviews,
} from "@/features/applications/api/previews";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import { useServersStore } from "@/features/servers";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import { pipelineStepsFor } from "@/features/applications/utils/deployPipeline";
import { createRequestGeneration } from "@/shared/utils/requestGeneration";

/**
 * State, data loading and mutations behind the application detail page.
 * The page itself only handles route params, layout and tab composition.
 */
export function useApplicationDetail() {
  const route = useRoute();
  const message = useMessage();
  const appsStore = useApplicationsStore();
  const serversStore = useServersStore();

  const appId = computed<string>(() => String(route.params.id ?? ""));
  const activeTab = ref("overview");
  const logDeploymentId = ref<string>("");
  const logServerId = ref<string>("");
  const rollbackOpen = ref(false);
  const rollbackTarget = ref<string>("");
  const rollingBack = ref(false);
  const envDraft = ref<EnvVar[]>([]);
  const envLoading = ref(false);
  const envError = ref<string | null>(null);
  // `envLoadedFor` names the application whose environment actually loaded. Save
  // is only enabled while it matches the current application, so an empty draft
  // from a failed read (or a draft left over from another application) can never
  // be written over unknown data.
  const envLoadedFor = ref<string>("");
  const storagesDraft = ref<StorageMapping[]>([]);
  const storagesLoading = ref(false);
  const storagesError = ref<string | null>(null);
  const storagesLoadedFor = ref<string>("");

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
  const previewsError = ref<string | null>(null);
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
   * controlHint explains why stop/start are unavailable, if they are: an
   * in-flight deployment owns the container right now, or no deployment ever
   * started one (the backend would answer 404).
   */
  const controlHint = computed<string | null>(() => {
    if (active.value !== null) {
      return "A deployment is in progress. Wait for it to finish.";
    }
    if (!hasContainer.value) {
      return "No container to control yet. Deploy the application first.";
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

  /** logTarget resolves the deployment selected in the Logs tab. */
  const logTarget = computed<Deployment | null>(
    () => deployments.value.find((item) => item.id === logDeploymentId.value) ?? active.value ?? latest.value,
  );

  /** effectiveLogServerId prefers the application's node, then the selection. */
  const effectiveLogServerId = computed<string>(
    () =>
      logServerId.value ||
      application.value?.server_id ||
      serversStore.servers[0]?.id ||
      "",
  );

  const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
    serversStore.servers.map((server) => ({
      label: `${server.name} · ${server.ip}`,
      value: server.id,
    })),
  );

  const deploymentOptions = computed<Array<{ label: string; value: string }>>(() =>
    deployments.value.map((item) => ({
      label: `${item.id.slice(0, 8)} · ${item.kind} · ${item.state}`,
      value: item.id,
    })),
  );

  /** pipelineSteps maps the latest deployment onto done/active/todo/failed. */
  const pipelineSteps = computed(() => pipelineStepsFor(latest.value));

  /** fetchAll loads the application, its history, config and node list. */
  async function fetchAll(): Promise<void> {
    if (!appId.value) {
      return;
    }
    try {
      await appsStore.fetchApplication(appId.value);
    } catch {
      // The store already exposes the error; the alert renders it.
    }
    try {
      await appsStore.fetchDeployments(appId.value);
    } catch {
      // The store already exposes the error; the alert renders it.
    }
    void loadEnv();
    void loadStorages();
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
    envError.value = null;
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
      envError.value = describeApplicationError(error);
      envLoadedFor.value = "";
    } finally {
      if (draftGeneration.isCurrent(token) && target === appId.value) {
        envLoading.value = false;
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
    storagesError.value = null;
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
      storagesError.value = describeApplicationError(error);
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
    previewsError.value = null;
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
      previewsError.value = describePreviewError(error);
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
    envError.value = null;
    try {
      const saved = await appsStore.saveEnv(target, envDraft.value);
      // Guard by generation as well as id: A→B→A must not let A's old save
      // response overwrite the draft B's navigation reloaded.
      if (target !== appId.value || !draftGeneration.isCurrent(token)) {
        return;
      }
      envDraft.value = [...saved];
      message.success("Environment saved. New variables apply to the next deploy.");
    } catch (error) {
      if (target !== appId.value || !draftGeneration.isCurrent(token)) {
        return;
      }
      envError.value = describeApplicationError(error);
    }
  }

  /** handleSaveStorages replaces the whole storage collection. */
  async function handleSaveStorages(): Promise<void> {
    const target = appId.value;
    if (target === "" || storagesLoadedFor.value !== target) {
      return;
    }
    const token = draftGeneration.current();
    storagesError.value = null;
    try {
      const saved = await appsStore.saveStorages(target, storagesDraft.value);
      if (target !== appId.value || !draftGeneration.isCurrent(token)) {
        return;
      }
      storagesDraft.value = [...saved];
      message.success("Volumes saved. They persist on the node across deploys.");
    } catch (error) {
      if (target !== appId.value || !draftGeneration.isCurrent(token)) {
        return;
      }
      storagesError.value = describeApplicationError(error);
    }
  }

  /** handleDeploy queues a redeploy of the current revision. */
  async function handleDeploy(): Promise<void> {
    try {
      await appsStore.deploy(appId.value);
      message.success("Deploy queued");
    } catch (error) {
      message.error(describeApplicationError(error));
    }
  }

  /** handleRollback queues a rollback to the selected running deployment. */
  async function handleRollback(): Promise<void> {
    rollingBack.value = true;
    try {
      await appsStore.rollback(appId.value, rollbackTarget.value || undefined);
      message.success("Rollback queued");
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
      message.success("Stop signal sent");
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
      message.success("Start signal sent");
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

  watch(appId, () => {
    // Invalidate any in-flight config read for the previous application.
    draftGeneration.bump();
    activeTab.value = "overview";
    containerStopped.value = false;
    stoppedForContainer.value = "";
    logDeploymentId.value = "";
    logServerId.value = "";
    envDraft.value = [];
    envError.value = null;
    envLoadedFor.value = "";
    envLoading.value = false;
    storagesDraft.value = [];
    storagesError.value = null;
    storagesLoadedFor.value = "";
    storagesLoading.value = false;
    previews.value = [];
    previewsLoaded.value = false;
    previewsError.value = null;
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
    void fetchAll();
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
    controlHint,
    containerStopped,
    containerIsRunning,
    pipelineSteps,
    logDeploymentId,
    logServerId,
    logTarget,
    effectiveLogServerId,
    serverOptions,
    deploymentOptions,
    rollbackOpen,
    rollbackTarget,
    rollingBack,
    envDraft,
    envLoading,
    envError,
    envLoadedFor,
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
    handleSaveEnv,
    handleSaveStorages,
    handleDeploy,
    handleRollback,
    handleStop,
    handleStart,
    openRollback,
    showLogsFor,
    openRollbackFor,
    loadEnv,
    loadStorages,
    loadPreviews,
  };
}

/** DetailState is the shared shape passed from the page to its tab panels. */
export type DetailState = ReturnType<typeof useApplicationDetail>;
