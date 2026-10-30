import { useMessage } from "naive-ui";
import { computed, inject, onMounted, onUnmounted, ref, watch } from "vue";
import type { ComputedRef, InjectionKey, Ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { deployStateLabel, describeServiceError, listServiceContainers } from "@/features/services/api/services";
import { activeLocale, i18n } from "@/shared/i18n";
import type {
  ComposeServiceContainer,
  Service,
  ServiceDeploy,
  UpdateServiceInput,
} from "@/features/services/api/services";
import { isApiError } from "@/features/servers";
import { resolveEnvironmentScope } from "@/features/projects/utils/canonicalRoutes";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { useServersStore } from "@/features/servers";
import { useServicesStore } from "@/features/services/stores/services";

export interface EnvRow {
  id: number;
  key: string;
  value: string;
}

export interface ServiceDetailContext {
  serviceId: ComputedRef<string>;
  service: ComputedRef<Service | null>;
  loading: Ref<boolean>;
  serverName: ComputedRef<string>;
  envReference: string;
  error: Ref<string | null>;
  notFound: Ref<boolean>;
  actionError: Ref<string | null>;
  busy: Ref<string | null>;
  controlHint: ComputedRef<string | null>;
  composeYaml: Ref<string>;
  composeEditing: Ref<boolean>;
  composeSaving: Ref<boolean>;
  composeError: Ref<string | null>;
  canEditCurrent: ComputedRef<boolean>;
  envDraft: Ref<EnvRow[]>;
  envSaving: Ref<boolean>;
  envError: Ref<string | null>;
  containers: Ref<ComposeServiceContainer[]>;
  containersLoading: Ref<boolean>;
  containersError: Ref<string | null>;
  containersLoaded: Ref<boolean>;
  deploys: ComputedRef<ServiceDeploy[]>;
  latestDeploy: ComputedRef<ServiceDeploy | null>;
  isDeploying: ComputedRef<boolean>;
  failedDeploy: ComputedRef<ServiceDeploy | null>;
  historyLoaded: ComputedRef<boolean>;
  historyUnavailable: ComputedRef<string | null>;
  historyLoading: ComputedRef<boolean>;
  loggableServices: ComputedRef<string[]>;
  canWrite: ComputedRef<boolean>;
  moveSaving: Ref<boolean>;
  moveError: Ref<string | null>;
  handleMove(_scope: { projectId: string; environmentId: string; serverId: string }): Promise<void>;
  handleSaveCompose(_text: string): Promise<void>;
  addEnvRow(): void;
  updateEnvRow(_id: number, _patch: Partial<EnvRow>): void;
  removeEnvRow(_id: number): void;
  handleSaveEnv(): Promise<void>;
  loadContainers(): Promise<void>;
  retryHistory(): Promise<void>;
  handleDeploy(): Promise<void>;
  handleStop(): Promise<void>;
  handleRestart(): Promise<void>;
  handleStart(): Promise<void>;
  handleDelete(): Promise<void>;
}

export const serviceDetailKey: InjectionKey<ServiceDetailContext> =
  Symbol("service-detail");

export function useServiceDetailContext(): ServiceDetailContext {
  const context = inject(serviceDetailKey);
  if (!context) {
    throw new Error("useServiceDetailContext must be used inside ServiceDetailPage.");
  }
  return context;
}

export function useServiceDetail(): ServiceDetailContext {
  const route = useRoute();
  const router = useRouter();
  const message = useMessage();
  const servicesStore = useServicesStore();
  const projectsStore = useProjectsStore();
  const serversStore = useServersStore();

  const envReference = "${VAR}";

  let nextEnvRowId = 0;

  const serviceId = computed<string>(() => String(route.params.id ?? ""));

  const loading = ref(false);
  const loadFailure: Ref<unknown> = ref(null);
  const notFound = ref(false);

  const composeYaml = ref("");
  const composeEditing = ref(false);
  const composeSaving = ref(false);
  const composeFailure: Ref<unknown> = ref(null);

  const envDraft = ref<EnvRow[]>([]);
  const envSaving = ref(false);
  const envFailure: Ref<unknown> = ref(null);

  const containers = ref<ComposeServiceContainer[]>([]);
  const containersLoading = ref(false);
  const containersFailure: Ref<unknown> = ref(null);
  const containersLoaded = ref(false);

  const busy = ref<string | null>(null);
  const actionFailure: Ref<unknown> = ref(null);

  function retained(failure: Ref<unknown>): ComputedRef<string | null> {
    return computed<string | null>(() => {
      if (failure.value === null) {
        return null;
      }
      void activeLocale.value;
      return describeServiceError(failure.value);
    });
  }

  const error = retained(loadFailure);
  const composeError = retained(composeFailure);
  const envError = retained(envFailure);
  const containersError = retained(containersFailure);
  const actionError = retained(actionFailure);

  const service = computed(() => servicesStore.serviceOf(serviceId.value));
  const deploys = computed<ServiceDeploy[]>(() =>
    servicesStore.deploysOf(serviceId.value),
  );
  const latestDeploy = computed<ServiceDeploy | null>(() => deploys.value[0] ?? null);
  // A service can sit in `creating` forever when it was imported without a
  // deploy; only an active deploy row makes the transitional state real.
  const isDeploying = computed<boolean>(
    () => latestDeploy.value?.state === "deploying",
  );
  const failedDeploy = computed<ServiceDeploy | null>(() => {
    const latest = latestDeploy.value;
    return latest?.state === "failed" ? latest : null;
  });

  const loadedServiceId = ref<string>("");

  let loadToken = 0;

  const canEditCurrent = computed<boolean>(
    () => loadedServiceId.value !== "" && loadedServiceId.value === serviceId.value,
  );

  const history = computed(() => servicesStore.historyOf(serviceId.value));
  const historyLoaded = computed<boolean>(() => history.value?.loaded ?? false);
  const historyUnavailable = computed<string | null>(() => {
    const failure = history.value?.failure ?? null;
    if (failure !== null) {
      void activeLocale.value;
      return describeServiceError(failure);
    }
    return history.value?.error ?? null;
  });
  const historyLoading = computed<boolean>(() => history.value?.loading ?? false);

  const serverName = computed<string>(() => {
    const current = service.value;
    if (!current) {
      return "—";
    }
    const server = serversStore.servers.find((item) => item.id === current.server_id);
        if (server) {
      return server.name;
    }
    void activeLocale.value;
    return String(i18n.global.t("services.overview.unknownNode"));
  });

  const loggableServices = computed<string[]>(() => {
    const names = new Set<string>();
    for (const route of service.value?.domains ?? []) {
      names.add(route.service);
    }
    for (const container of containers.value) {
      names.add(container.service);
    }
    return [...names].sort();
  });

  function envRows(env: Record<string, string>): EnvRow[] {
    return Object.entries(env).map(([key, value]) => ({
      id: ++nextEnvRowId,
      key,
      value,
    }));
  }

  async function load(): Promise<void> {
    const token = ++loadToken;
    const id = serviceId.value;
    loading.value = true;
    loadedServiceId.value = "";
    loadFailure.value = null;
    notFound.value = false;
    actionFailure.value = null;
    moveSaving.value = false;
    moveFailure.value = null;
    composeFailure.value = null;
    composeEditing.value = false;
    envFailure.value = null;
    containers.value = [];
    containersFailure.value = null;
    containersLoaded.value = false;
    composeYaml.value = "";
    envDraft.value = [];
    try {
      const fetched = await servicesStore.fetchService(id);
      if (token !== loadToken) {
        return;
      }
      const canonical = resolveEnvironmentScope(
        { projectId: fetched.project_id, environmentId: fetched.environment_id },
        {
          projectId: String(route.params.projectId ?? ""),
          environmentId: String(route.params.environmentId ?? ""),
        },
      );
      if (canonical !== null) {
        await router.replace({
          name: "service-detail",
          params: { projectId: canonical.projectId, environmentId: canonical.environmentId, id },
        });
        return;
      }
      composeYaml.value = fetched.compose_yaml ?? "";
      envDraft.value = envRows(fetched.env ?? {});
      loadedServiceId.value = id;
      await servicesStore.fetchDeploys(id).catch(() => undefined);
      void serversStore.fetchServers().catch(() => undefined);
    } catch (err) {
      if (token !== loadToken) {
        return;
      }
      loadFailure.value = err;
      notFound.value = isApiError(err) && err.status === 404;
    } finally {
      if (token === loadToken) {
        loading.value = false;
      }
    }
  }

  async function retryHistory(): Promise<void> {
    await servicesStore.fetchDeploys(serviceId.value).catch(() => undefined);
  }

  async function handleSaveCompose(text: string): Promise<void> {
    const id = serviceId.value;
    if (!canEditCurrent.value || id === "") {
      return;
    }
    const token = loadToken;
    composeSaving.value = true;
    composeFailure.value = null;
    try {
      const updated = await servicesStore.update(id, { compose_yaml: text });
      if (token !== loadToken || id !== serviceId.value) {
        return;
      }
      composeYaml.value = updated.compose_yaml ?? text;
      composeEditing.value = false;
      message.success(String(i18n.global.t("services.toast.composeSaved")));
    } catch (err) {
      if (token === loadToken && id === serviceId.value) {
        composeFailure.value = err;
      }
    } finally {
      composeSaving.value = false;
    }
  }

  function addEnvRow(): void {
    envDraft.value = [
      ...envDraft.value,
      { id: ++nextEnvRowId, key: "", value: "" },
    ];
  }

  function updateEnvRow(id: number, patch: Partial<EnvRow>): void {
    envDraft.value = envDraft.value.map((row) =>
      row.id === id ? { ...row, ...patch } : row,
    );
  }

  function removeEnvRow(id: number): void {
    envDraft.value = envDraft.value.filter((row) => row.id !== id);
  }

  async function handleSaveEnv(): Promise<void> {
    const id = serviceId.value;
    if (!canEditCurrent.value || id === "") {
      return;
    }
    const env: Record<string, string> = {};
    const reservedDomainKey = ["GOTHAM", "DOMAIN"].join("_");
    for (const row of envDraft.value) {
      const key = row.key.trim();
      if (key === reservedDomainKey || key === `${reservedDomainKey}S`) {
        message.error(
          String(i18n.global.t("services.env.reservedKey", { key })),
        );
        return;
      }
      if (key !== "") {
        env[key] = row.value;
      }
    }
    const token = loadToken;
    envSaving.value = true;
    envFailure.value = null;
    try {
      const updated = await servicesStore.update(id, { env });
      if (token !== loadToken || id !== serviceId.value) {
        return;
      }
      envDraft.value = envRows(updated.env ?? {});
      message.success(String(i18n.global.t("services.toast.envSaved")));
    } catch (err) {
      if (token === loadToken && id === serviceId.value) {
        envFailure.value = err;
      }
    } finally {
      envSaving.value = false;
    }
  }

  async function loadContainers(): Promise<void> {
    containersLoading.value = true;
    containersFailure.value = null;
    try {
      containers.value = await listServiceContainers(serviceId.value);
      containersLoaded.value = true;
    } catch (err) {
      containers.value = [];
      containersFailure.value = err;
    } finally {
      containersLoading.value = false;
    }
  }

  async function handleDeploy(): Promise<void> {
    busy.value = "deploy";
    actionFailure.value = null;
    try {
      const outcome = await servicesStore.deploy(serviceId.value);
      message.success(String(i18n.global.t("services.toast.deployStarted", { state: deployStateLabel(outcome.deploy.state) })));
    } catch (err) {
      actionFailure.value = err;
    } finally {
      busy.value = null;
    }
  }

  async function handleStop(): Promise<void> {
    busy.value = "stop";
    actionFailure.value = null;
    try {
      await servicesStore.stop(serviceId.value);
      message.success(String(i18n.global.t("services.toast.stopped")));
    } catch (err) {
      actionFailure.value = err;
    } finally {
      busy.value = null;
    }
  }

  async function handleRestart(): Promise<void> {
    busy.value = "restart";
    actionFailure.value = null;
    try {
      await servicesStore.restart(serviceId.value);
      message.success(String(i18n.global.t("services.toast.restartSent")));
    } catch (err) {
      actionFailure.value = err;
    } finally {
      busy.value = null;
    }
  }

  async function handleStart(): Promise<void> {
    busy.value = "start";
    actionFailure.value = null;
    try {
      await servicesStore.restart(serviceId.value);
      message.success(String(i18n.global.t("services.toast.started")));
    } catch (err) {
      actionFailure.value = err;
    } finally {
      busy.value = null;
    }
  }

  async function handleDelete(): Promise<void> {
    const current = service.value;
    busy.value = "delete";
    actionFailure.value = null;
    try {
      const warnings = await servicesStore.remove(serviceId.value);
      if (warnings.length > 0) {
        message.warning(
          String(
            i18n.global.t("services.toast.deletedWithWarnings", {
              warnings: warnings.join("; "),
            }),
          ),
        );
      } else {
        message.success(String(i18n.global.t("services.toast.deleted")));
      }
      if (current) {
        await router.push({
          name: "environment-detail",
          params: { projectId: current.project_id, environmentId: current.environment_id },
        });
      } else {
        await router.push({ name: "projects" });
      }
    } catch (err) {
      actionFailure.value = err;
    } finally {
      busy.value = null;
    }
  }

  const canWrite = computed<boolean>(() => projectsStore.canWrite);

  const controlHint = computed<string | null>(() => {
    void activeLocale.value;
    if (isDeploying.value) {
      return String(i18n.global.t("services.header.controlDeployInProgress"));
    }
    if (!canWrite.value) {
      return String(i18n.global.t("services.header.controlReadOnly"));
    }
    if (busy.value !== null) {
      return String(i18n.global.t("services.header.controlInProgress"));
    }
    const serverId = service.value?.server_id;
    if (
      serverId &&
      serversStore.servers.some(
        (server) => server.id === serverId && server.status === "offline",
      )
    ) {
      return String(i18n.global.t("services.header.controlNodeOffline"));
    }
    return null;
  });

  const moveSaving = ref(false);
  const moveFailure: Ref<unknown> = ref(null);
  const moveError = retained(moveFailure);

  async function handleMove(scope: {
    projectId: string;
    environmentId: string;
    serverId: string;
  }): Promise<void> {
    const current = service.value;
    if (!current) {
      return;
    }
    const input: UpdateServiceInput = {};
    if (scope.environmentId !== current.environment_id) {
      input.environment_id = scope.environmentId;
    }
    if (scope.serverId !== current.server_id) {
      input.server_id = scope.serverId;
    }
    if (Object.keys(input).length === 0) {
      return;
    }
    const targetId = serviceId.value;
    moveSaving.value = true;
    moveFailure.value = null;
    try {
      const updated = await servicesStore.update(targetId, input);
      if (targetId !== serviceId.value) {
        return;
      }
      message.success(String(i18n.global.t("services.toast.locationSaved")));
      await refreshProjectCounts([current.project_id, updated.project_id]);
      if (targetId !== serviceId.value) {
        return;
      }
      if (input.environment_id) {
        await router.push({
          name: "service-detail",
          params: {
            projectId: updated.project_id,
            environmentId: updated.environment_id,
            id: updated.id,
          },
        });
      }
    } catch (err) {
      if (targetId === serviceId.value) {
        moveFailure.value = err;
      }
    } finally {
      if (targetId === serviceId.value) {
        moveSaving.value = false;
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

  onMounted(() => {
    void load();
  });

  const deployPollIntervalMs = 5_000;
  let deployPollTimer: ReturnType<typeof setInterval> | null = null;

  function stopDeployPolling(): void {
    if (deployPollTimer !== null) {
      clearInterval(deployPollTimer);
      deployPollTimer = null;
    }
  }

  async function refreshDeployState(): Promise<void> {
    const id = serviceId.value;
    if (id === "") {
      return;
    }
    await servicesStore.fetchService(id).catch(() => undefined);
    await servicesStore.fetchDeploys(id).catch(() => undefined);
  }

  // Follow a background deploy until its row is terminal; the server holds the
  // lifecycle lock for that whole time, so nothing else polls the service.
  watch(
    isDeploying,
    (deploying) => {
      if (!deploying) {
        stopDeployPolling();
        return;
      }
      if (deployPollTimer === null) {
        deployPollTimer = setInterval(() => {
          void refreshDeployState();
        }, deployPollIntervalMs);
      }
    },
    { immediate: true },
  );

  onUnmounted(stopDeployPolling);

  watch([serviceId, () => String(route.params.environmentId ?? "")], () => {
    stopDeployPolling();
    void load();
  });

  return {
    serviceId,
    service,
    loading,
    serverName,
    envReference,
    error,
    notFound,
    actionError,
    busy,
    controlHint,
    composeYaml,
    composeEditing,
    composeSaving,
    composeError,
    canEditCurrent,
    envDraft,
    envSaving,
    envError,
    containers,
    containersLoading,
    containersError,
    containersLoaded,
    deploys,
    latestDeploy,
    isDeploying,
    failedDeploy,
    historyLoaded,
    historyUnavailable,
    historyLoading,
    loggableServices,
    canWrite,
    moveSaving,
    moveError,
    handleMove,
    handleSaveCompose,
    addEnvRow,
    updateEnvRow,
    removeEnvRow,
    handleSaveEnv,
    loadContainers,
    retryHistory,
    handleDeploy,
    handleStop,
    handleRestart,
    handleStart,
    handleDelete,
  };
}
