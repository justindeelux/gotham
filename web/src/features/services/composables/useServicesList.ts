import { useMessage } from "naive-ui";
import { computed, inject, onMounted, ref } from "vue";
import type { ComputedRef, InjectionKey, Ref } from "vue";
import { useRouter } from "vue-router";

import { describeServiceError } from "@/features/services/api/services";
import type { Service } from "@/features/services/api/services";
import { serviceNameSchema, serviceNodeSchema } from "@/shared/validation/primitives";
import { fieldErrors } from "@/shared/validation/naiveAdapter";
import { useServersStore } from "@/features/servers";
import { useServicesStore } from "@/features/services/stores/services";
import { useTemplatesStore } from "@/features/templates";
import { relativeTime } from "@/shared/utils/format";

export type StatusFilter = "all" | "running" | "stopped";

/**
 * Shared page context for the services list surface. Created once by
 * ServicesPage and consumed by its toolbar, list panel and dialogs through
 * `useServicesPageContext`, so no prop drilling is needed.
 */
export interface ServicesPageContext {
  activeTab: Ref<string>;
  statusFilter: Ref<StatusFilter>;
  search: Ref<string>;
  importOpen: Ref<boolean>;
  importName: Ref<string>;
  importServerId: Ref<string>;
  importYaml: Ref<string>;
  importAttempted: Ref<boolean>;
  importing: Ref<boolean>;
  importError: Ref<string | null>;
  wizardOpen: Ref<boolean>;
  wizardSlug: Ref<string>;
  envReference: string;
  templatePlaceholder: string;
  filters: Array<{ key: StatusFilter; label: string }>;
  counts: ComputedRef<Record<StatusFilter, number>>;
  filteredServices: ComputedRef<Service[]>;
  serverOptions: ComputedRef<Array<{ label: string; value: string }>>;
  failedHistories: ComputedRef<Service[]>;
  importNameError: ComputedRef<string>;
  importNodeError: ComputedRef<string>;
  serverNameOf(_service: Service): string;
  deploySummary(_service: Service): string;
  deployTagType(_service: Service): "default" | "warning";
  retryHistories(): Promise<void>;
  openImport(): void;
  handleImport(): Promise<void>;
  openWizard(_slug: string): void;
  handleDelete(_service: Service): Promise<void>;
}

export const servicesPageKey: InjectionKey<ServicesPageContext> = Symbol("services-page");

/** useServicesPageContext reads the page context provided by ServicesPage. */
export function useServicesPageContext(): ServicesPageContext {
  const context = inject(servicesPageKey);
  if (!context) {
    throw new Error("useServicesPageContext must be used inside ServicesPage.");
  }
  return context;
}

/**
 * useServicesList owns the services list state: status/search filtering, the
 * compose import dialog, the template wizard selection, and the list-level
 * loads and deletions.
 */
export function useServicesList(): ServicesPageContext {
  const router = useRouter();
  const message = useMessage();
  const servicesStore = useServicesStore();
  const templatesStore = useTemplatesStore();
  const serversStore = useServersStore();

  const activeTab = ref("compose");
  const statusFilter = ref<StatusFilter>("all");
  const search = ref("");

  const importOpen = ref(false);
  const importName = ref("");
  const importServerId = ref("");
  const importYaml = ref("");
  const importAttempted = ref(false);
  const importing = ref(false);
  const importError = ref<string | null>(null);

  /** envReference is the compose `${VAR}` substitution form shown in copy. */
  const envReference = "${VAR}";

  const wizardOpen = ref(false);
  const wizardSlug = ref("");

  /** templatePlaceholder is the engine's only supported placeholder form. */
  const templatePlaceholder = "{{ .field }}";

  const filters: Array<{ key: StatusFilter; label: string }> = [
    { key: "all", label: "All" },
    { key: "running", label: "Running" },
    { key: "stopped", label: "Stopped" },
  ];

  const counts = computed<Record<StatusFilter, number>>(() => ({
    all: servicesStore.services.length,
    running: servicesStore.services.filter((item) => item.status === "running").length,
    stopped: servicesStore.services.filter((item) => item.status === "stopped").length,
  }));

  /** serverNameOf resolves a service's node to its display name. */
  function serverNameOf(service: Service): string {
    const server = serversStore.servers.find((item) => item.id === service.server_id);
    return server ? server.name : "unknown node";
  }

  /** filteredServices applies the status chip and the free-text search. */
  const filteredServices = computed<Service[]>(() => {
    const term = search.value.trim().toLowerCase();
    return servicesStore.services.filter((service) => {
      if (statusFilter.value !== "all" && service.status !== statusFilter.value) {
        return false;
      }
      if (term === "") {
        return true;
      }
      const haystack = [
        service.name,
        service.project_name,
        serverNameOf(service),
        ...service.domains.map((route) => route.domain),
      ]
        .join(" ")
        .toLowerCase();
      return haystack.includes(term);
    });
  });

  const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
    serversStore.servers.map((server) => ({
      label: `${server.name} · ${server.ip}`,
      value: server.id,
    })),
  );

  /** failedHistories lists services whose deploy history could not be read. */
  const failedHistories = computed<Service[]>(() =>
    servicesStore.services.filter(
      (service) => servicesStore.historyOf(service.id)?.error,
    ),
  );

  const importNameError = computed<string>(() =>
    !importAttempted.value ? "" : (fieldErrors(serviceNameSchema, importName.value)[0] ?? ""),
  );

  const importNodeError = computed<string>(() =>
    !importAttempted.value ? "" : (fieldErrors(serviceNodeSchema, importServerId.value)[0] ?? ""),
  );

  /**
   * deploySummary describes the newest deploy attempt. A history that was never
   * read or failed to read is labelled as such: "no deploys yet" is only claimed
   * after a successful empty read, and cached rows keep their summary with a
   * stale marker when a refresh fails.
   */
  function deploySummary(service: Service): string {
    const history = servicesStore.historyOf(service.id);
    const attempts = history?.deploys ?? [];
    if (attempts.length > 0) {
      const latest = attempts[0];
      const finished = latest.finished_at
        ? ` · ${relativeTime(latest.finished_at)}`
        : "";
      const stale = history?.error ? " · stale" : "";
      return `deploy #${attempts.length} · ${latest.state}${finished}${stale}`;
    }
    if (history?.error) {
      return "deploy history unavailable";
    }
    if (history?.loaded) {
      return "no deploys yet";
    }
    return "deploy history loading…";
  }

  /** deployTagType marks the summary tag when its history is unavailable. */
  function deployTagType(service: Service): "default" | "warning" {
    return servicesStore.historyOf(service.id)?.error ? "warning" : "default";
  }

  /** retryHistories re-reads every failed deploy history. */
  async function retryHistories(): Promise<void> {
    await servicesStore.fetchAllDeploys();
  }

  /** load refreshes services, deploys, templates and the node list. */
  async function load(): Promise<void> {
    await Promise.allSettled([
      servicesStore.fetchServices(),
      templatesStore.fetchTemplates(),
      serversStore.fetchServers(),
    ]);
    await servicesStore.fetchAllDeploys();
  }

  /** openImport resets the import dialog. */
  function openImport(): void {
    importName.value = "";
    importServerId.value = serversStore.servers[0]?.id ?? "";
    importYaml.value = "";
    importAttempted.value = false;
    importError.value = null;
    importOpen.value = true;
  }

  /** handleImport stores the pasted document as a new service. */
  async function handleImport(): Promise<void> {
    importAttempted.value = true;
    if (
      !serviceNameSchema.safeParse(importName.value).success ||
      !serviceNodeSchema.safeParse(importServerId.value).success
    ) {
      return;
    }
    importing.value = true;
    importError.value = null;
    try {
      const created = await servicesStore.create({
        name: importName.value.trim(),
        server_id: importServerId.value,
        compose_yaml: importYaml.value,
      });
      message.success(`Service ${created.name} created.`);
      importOpen.value = false;
      await router.push({ name: "service-detail", params: { id: created.id } });
    } catch (error) {
      importError.value = describeServiceError(error);
    } finally {
      importing.value = false;
    }
  }

  /** openWizard opens the template wizard for one gallery card. */
  function openWizard(slug: string): void {
    wizardSlug.value = slug;
    wizardOpen.value = true;
  }

  /** handleDelete soft-deletes a service; named volumes stay on the node. */
  async function handleDelete(service: Service): Promise<void> {
    try {
      await servicesStore.remove(service.id);
      message.success(`Service ${service.name} deleted. Named volumes were kept.`);
    } catch (error) {
      message.error(describeServiceError(error));
    }
  }

  onMounted(() => {
    void load();
  });

  return {
    activeTab,
    statusFilter,
    search,
    importOpen,
    importName,
    importServerId,
    importYaml,
    importAttempted,
    importing,
    importError,
    wizardOpen,
    wizardSlug,
    envReference,
    templatePlaceholder,
    filters,
    counts,
    filteredServices,
    serverOptions,
    failedHistories,
    importNameError,
    importNodeError,
    serverNameOf,
    deploySummary,
    deployTagType,
    retryHistories,
    openImport,
    handleImport,
    openWizard,
    handleDelete,
  };
}
