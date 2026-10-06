import { useMessage } from "naive-ui";
import { computed, inject, onMounted, ref, watch } from "vue";
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

/**
 * One editable environment row. Values are masked (see the template note).
 * Rows carry a stable id so add/remove keeps focus and input state attached to
 * the row it belongs to instead of its position.
 */
export interface EnvRow {
  id: number;
  key: string;
  value: string;
}

/**
 * Shared page context for the service detail surface. Created once by
 * ServiceDetailPage and consumed by its cards through
 * `useServiceDetailContext`, so no prop drilling is needed.
 */
export interface ServiceDetailContext {
  serviceId: ComputedRef<string>;
  service: ComputedRef<Service | null>;
  serverName: ComputedRef<string>;
  envReference: string;
  error: Ref<string | null>;
  notFound: Ref<boolean>;
  actionError: Ref<string | null>;
  busy: Ref<string | null>;
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
  handleDelete(): Promise<void>;
}

export const serviceDetailKey: InjectionKey<ServiceDetailContext> =
  Symbol("service-detail");

/** useServiceDetailContext reads the page context of ServiceDetailPage. */
export function useServiceDetailContext(): ServiceDetailContext {
  const context = inject(serviceDetailKey);
  if (!context) {
    throw new Error("useServiceDetailContext must be used inside ServiceDetailPage.");
  }
  return context;
}

/**
 * useServiceDetail owns the detail page state: the guarded detail load, the
 * compose and environment drafts with their ownership-gated saves, the
 * on-demand containers read, the lifecycle actions and the deploy history
 * reads. See the original ServiceDetailPage for the token protocol.
 */
export function useServiceDetail(): ServiceDetailContext {
  const route = useRoute();
  const router = useRouter();
  const message = useMessage();
  const servicesStore = useServicesStore();
  const projectsStore = useProjectsStore();
  const serversStore = useServersStore();

  /** envReference is the compose `${VAR}` substitution form shown in copy. */
  const envReference = "${VAR}";

  /** Monotonic env-row id for stable list keys. */
  let nextEnvRowId = 0;

  const serviceId = computed<string>(() => String(route.params.id ?? ""));

  /**
   * Raw refusals retained per surface; each display error derives from its
   * refusal in the active locale (see retained below), so an open banner
   * refreshes on a language switch without losing drafts or refetching.
   */
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

  /** retained derives display text from a raw refusal in the active locale. */
  function retained(failure: Ref<unknown>): ComputedRef<string | null> {
    return computed<string | null>(() => {
      if (failure.value === null) {
        return null;
      }
      // Tracks the locale when called during render or inside a computed.
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

  /**
   * loadedServiceId is the service whose configuration is in the editors right
   * now. It is set only by a successful detail read for the id the load started
   * with, so a pending or failed load can never leave another service's drafts
   * editable — the compose/env saves are gated on it.
   */
  const loadedServiceId = ref<string>("");

  /**
   * loadToken invalidates obsolete detail loads: a newer load (route change,
   * reload) bumps it, and a completion whose token no longer matches is dropped
   * before it can touch a page-local draft.
   */
  let loadToken = 0;

  /** canEditCurrent reports whether the drafts belong to the displayed service. */
  const canEditCurrent = computed<boolean>(
    () => loadedServiceId.value !== "" && loadedServiceId.value === serviceId.value,
  );

  /**
   * history is the per-service deploy-history state. A missing entry means the
   * history was never read: the UI must not claim it is empty then, and only
   * `loaded` (set by a successful read) allows the empty/zero copy.
   */
  const history = computed(() => servicesStore.historyOf(serviceId.value));
  const historyLoaded = computed<boolean>(() => history.value?.loaded ?? false);
  const historyUnavailable = computed<string | null>(() => {
    const failure = history.value?.failure ?? null;
    if (failure !== null) {
      // Tracks the locale when called during render or inside a computed.
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
    // Tracks the locale when called during render or inside a computed.
    void activeLocale.value;
    return String(i18n.global.t("services.overview.unknownNode"));
  });

  /**
   * loggableServices lists the compose service selectors the UI knows about:
   * the routed services declared by the stored document, plus the project's
   * container services once they were read from the node.
   */
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

  /** envRows converts the API's environment map into editable rows. */
  function envRows(env: Record<string, string>): EnvRow[] {
    return Object.entries(env).map(([key, value]) => ({
      id: ++nextEnvRowId,
      key,
      value,
    }));
  }

  /** load fetches one service, its history and the node list. */
  async function load(): Promise<void> {
    const token = ++loadToken;
    // Capture the id this load belongs to: the route may change while it awaits,
    // and an obsolete completion must never write another service's drafts.
    const id = serviceId.value;
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
      // The row ids are the authority: a wrong project/environment in the
      // URL replaces it with the canonical nested URL instead of rendering
      // silently. The route watcher reloads from there.
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
    } catch (err) {
      if (token !== loadToken) {
        return;
      }
      loadFailure.value = err;
      notFound.value = isApiError(err) && err.status === 404;
      return;
    }
    // Dependent reads use the captured id: they belong to the service this load
    // started for, never to whatever the route shows now.
    await servicesStore.fetchDeploys(id).catch(() => undefined);
    void serversStore.fetchServers().catch(() => undefined);
  }

  /** retryHistory re-reads the deploy history after an unavailable result. */
  async function retryHistory(): Promise<void> {
    await servicesStore.fetchDeploys(serviceId.value).catch(() => undefined);
  }

  /** handleSaveCompose persists the edited document (PATCH → 200). */
  async function handleSaveCompose(text: string): Promise<void> {
    // A save may only fire for the service whose configuration is displayed: the
    // drafts are cleared while a load is pending, so an unguarded save could
    // write an empty or foreign document onto the route's service.
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
        // The route moved on while the write was in flight; the response belongs
        // to the previous service and must not seed the new drafts.
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

  /** addEnvRow appends an empty variable row. */
  function addEnvRow(): void {
    envDraft.value = [
      ...envDraft.value,
      { id: ++nextEnvRowId, key: "", value: "" },
    ];
  }

  /** updateEnvRow replaces one row immutably, addressed by its stable id. */
  function updateEnvRow(id: number, patch: Partial<EnvRow>): void {
    envDraft.value = envDraft.value.map((row) =>
      row.id === id ? { ...row, ...patch } : row,
    );
  }

  /** removeEnvRow drops one row, addressed by its stable id. */
  function removeEnvRow(id: number): void {
    envDraft.value = envDraft.value.filter((row) => row.id !== id);
  }

  /** handleSaveEnv replaces the whole environment map (PATCH → 200). */
  async function handleSaveEnv(): Promise<void> {
    // Same draft-ownership guard as the compose save.
    const id = serviceId.value;
    if (!canEditCurrent.value || id === "") {
      return;
    }
    const env: Record<string, string> = {};
    for (const row of envDraft.value) {
      const key = row.key.trim();
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

  /** loadContainers reads the project's containers from the node on demand. */
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

  /** handleDeploy renders the stored document and starts the project. */
  async function handleDeploy(): Promise<void> {
    busy.value = "deploy";
    actionFailure.value = null;
    try {
      const outcome = await servicesStore.deploy(serviceId.value);
      message.success(String(i18n.global.t("services.toast.deployState", { state: deployStateLabel(outcome.deploy.state) })));
    } catch (err) {
      actionFailure.value = err;
    } finally {
      busy.value = null;
    }
  }

  /** handleStop takes the project down; named volumes keep their data. */
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

  /** handleRestart restarts a running project, or starts a stopped one. */
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

  /** handleDelete stops the project and removes the row. */
  async function handleDelete(): Promise<void> {
    const current = service.value;
    busy.value = "delete";
    actionFailure.value = null;
    try {
      await servicesStore.remove(serviceId.value);
      message.success(String(i18n.global.t("services.toast.deleted")));
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

  /** canWrite follows the contract's roles: viewers read, members write. */
  const canWrite = computed<boolean>(() => projectsStore.canWrite);

  const moveSaving = ref(false);
  /** moveFailure retains the raw move refusal; moveError derives its display. */
  const moveFailure: Ref<unknown> = ref(null);
  const moveError = retained(moveFailure);

  /**
   * handleMove applies the location settings (move environment, change
   * node). Only changed fields ride the PATCH; a move retargets the nested
   * route to the new environment. The contract's 409 refusals (in-flight
   * deploy, deployed service, name collision) render inline through
   * moveError. The navigation is identity-guarded: leaving the service
   * mid-request never yanks the user back to it.
   */
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
        return; // the route moved on while the write was in flight
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

  onMounted(() => {
    void load();
  });

  // The detail route is reused when navigating between services, and a move
  // keeps the id while the environment changes: reload on either.
  watch([serviceId, () => String(route.params.environmentId ?? "")], () => {
    void load();
  });

  return {
    serviceId,
    service,
    serverName,
    envReference,
    error,
    notFound,
    actionError,
    busy,
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
    handleDelete,
  };
}
