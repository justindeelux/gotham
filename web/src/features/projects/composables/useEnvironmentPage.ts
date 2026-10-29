import { computed, onMounted, ref, watch } from "vue";
import type { Ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import type { DeploymentState } from "@/features/applications/api/applications";
import {
  latestDeploymentStates,
} from "@/features/applications/api/applications";
import { applicationBrand, databaseBrand, serviceBrand } from "@/features/add-resource";
import type { BrandMark } from "@/features/add-resource";
import {
  describeProjectError,
  getEnvironmentResources,
  getProject,
} from "@/features/projects/api/projects";
import { activeLocale, i18n } from "@/shared/i18n";
import type {
  Environment,
  EnvironmentResourceApplication,
  EnvironmentResourceDatabase,
  EnvironmentResources,
} from "@/features/projects/api/projects";
import { resolveEnvironmentScope } from "@/features/projects/utils/canonicalRoutes";
import { serviceStatusTagType } from "@/features/services/api/services";
import type { Service } from "@/features/services/api/services";
import { isApiError } from "@/features/servers";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { createRequestGeneration } from "@/shared/utils/requestGeneration";
import type { RuntimeHealthState } from "@/shared/types/runtimeHealth";

export type EnvironmentResourceKind = "application" | "service" | "database";

export type EnvironmentResourceTab = "all" | "applications" | "services" | "databases";

export interface EnvironmentRow {
  kind: EnvironmentResourceKind;
  id: string;
  name: string;
  subtitle: string;
  brand: BrandMark | null;
  serverName: string;
  statusText: string;
  statusTag: "success" | "warning" | "error" | "default";
  runtimeState?: RuntimeHealthState;
  runtimeReason?: string;
  preview: boolean;
  to: { name: string; params: Record<string, string> };
}

function statusText(key: string): string {
  void activeLocale.value;
  return String(i18n.global.t(key));
}

export function applicationStatusView(
  state: DeploymentState | null | "unknown",
): { text: string; tag: EnvironmentRow["statusTag"] } {
  if (state === "unknown") {
    return { text: statusText("projects.environment.status.unknown"), tag: "default" };
  }
  if (state === null) {
    return { text: statusText("projects.environment.status.notDeployed"), tag: "default" };
  }
  if (state === "running") {
    return { text: statusText("projects.environment.status.running"), tag: "success" };
  }
  if (state === "failed") {
    return { text: statusText("projects.environment.status.failed"), tag: "error" };
  }
  return { text: statusText("projects.environment.status.deploying"), tag: "warning" };
}

export function databaseStatusView(
  status: string,
): { text: string; tag: EnvironmentRow["statusTag"] } {
  if (status === "running") {
    return { text: statusText("projects.environment.status.running"), tag: "success" };
  }
  if (status === "error") {
    return { text: statusText("projects.environment.status.error"), tag: "error" };
  }
  if (status === "creating") {
    return { text: statusText("projects.environment.status.creating"), tag: "warning" };
  }
  return {
    text: status === "" ? statusText("projects.environment.status.unknown") : status,
    tag: "default",
  };
}

export function applicationSubtitle(application: EnvironmentResourceApplication): string {
  const parts = [application.repo, application.branch].filter(
    (part) => part.trim() !== "",
  );
  return parts.length > 0 ? parts.join(" · ") : "—";
}

export function serviceSubtitle(service: Service): string {
  if (service.domains.length > 0) {
    return service.domains.map((route) => route.domain).join(", ");
  }
  return statusText("projects.environment.composeService");
}

export function databaseSubtitle(database: EnvironmentResourceDatabase): string {
  return database.version ? `${database.engine}:${database.version}` : database.engine;
}

export function buildEnvironmentRows(
  resources: EnvironmentResources,
  applicationStates: Record<string, DeploymentState | null>,
  applicationStatesFailed: boolean,
  projectId: string,
  environmentId: string,
): EnvironmentRow[] {
  const rows: EnvironmentRow[] = [];
  const previews = resources.applications.filter((application) => application.is_preview);
  const bases = resources.applications.filter((application) => !application.is_preview);
  const previewsOf = new Map<string, EnvironmentResourceApplication[]>();
  for (const preview of previews) {
    const baseId = preview.preview_of ?? "";
    const siblings = previewsOf.get(baseId) ?? [];
    siblings.push(preview);
    previewsOf.set(baseId, siblings);
  }
  const applicationRow = (application: EnvironmentResourceApplication, preview: boolean): EnvironmentRow => {
    const state = applicationStates[application.id] ?? null;
    const view = applicationStatusView(
      state === null && applicationStatesFailed ? "unknown" : state,
    );
    return {
      kind: "application",
      id: application.id,
      name: application.name,
      subtitle: applicationSubtitle(application),
      brand: applicationBrand(
        application.source_type ?? "",
        application.provider,
        application.name,
      ),
      serverName: application.server_name || statusText("projects.environment.unassigned"),
      statusText: view.text,
      statusTag: view.tag,
      runtimeState: application.runtime_health?.state,
      runtimeReason: application.runtime_health?.reason,
      preview,
      to: { name: "application-detail", params: { projectId, environmentId, id: application.id } },
    };
  };
  for (const application of bases) {
    rows.push(applicationRow(application, false));
    for (const preview of previewsOf.get(application.id) ?? []) {
      rows.push(applicationRow(preview, true));
    }
  }
  for (const [baseId, siblings] of previewsOf) {
    if (bases.some((application) => application.id === baseId)) {
      continue;
    }
    for (const preview of siblings) {
      rows.push(applicationRow(preview, true));
    }
  }
  for (const service of resources.services) {
    rows.push({
      kind: "service",
      id: service.id,
      name: service.name,
      subtitle: serviceSubtitle(service),
      brand: serviceBrand(service),
      serverName: service.server_name,
      statusText: service.status,
      statusTag: serviceStatusTagType(service.status),
      preview: false,
      to: { name: "service-detail", params: { projectId, environmentId, id: service.id } },
    });
  }
  for (const database of resources.databases) {
    const view = databaseStatusView(database.status);
    rows.push({
      kind: "database",
      id: database.id,
      name: database.name,
      subtitle: databaseSubtitle(database),
      brand: databaseBrand(database.engine, database.name),
      serverName: database.server_name,
      statusText: view.text,
      statusTag: view.tag,
      runtimeState: database.runtime_health?.state,
      runtimeReason: database.runtime_health?.reason,
      preview: false,
      to: { name: "database-detail", params: { projectId, environmentId, id: database.id } },
    });
  }
  return rows;
}

export function filterEnvironmentRows(
  rows: EnvironmentRow[],
  tab: EnvironmentResourceTab,
  query: string,
): EnvironmentRow[] {
  const needle = query.trim().toLowerCase();
  return rows.filter((row) => {
    if (tab === "applications" && row.kind !== "application") {
      return false;
    }
    if (tab === "services" && row.kind !== "service") {
      return false;
    }
    if (tab === "databases" && row.kind !== "database") {
      return false;
    }
    if (needle === "") {
      return true;
    }
    return [row.name, row.subtitle, row.serverName]
      .join(" ")
      .toLowerCase()
      .includes(needle);
  });
}

export function useEnvironmentPage() {
  const route = useRoute();
  const router = useRouter();
  const projectsStore = useProjectsStore();

  const projectId = computed<string>(() => String(route.params.projectId ?? ""));
  const environmentId = computed<string>(() => String(route.params.environmentId ?? ""));

  const loading = ref(false);
  const failure: Ref<unknown> = ref(null);
  const error = computed<string | null>(() =>
    failure.value === null ? null : describeProjectError(failure.value),
  );
  const notFound = ref(false);
  const resources = ref<EnvironmentResources | null>(null);
  const siblings = ref<Environment[]>([]);
  const applicationStates = ref<Record<string, DeploymentState | null>>({});
  const applicationStatesFailed = ref(false);
  const tab = ref<EnvironmentResourceTab>("all");
  const showPreviews = ref(false);
  const search = ref("");
  const addOpen = ref(false);
  const importOpen = ref(false);

  const reloadGeneration = createRequestGeneration();

  const rows = computed<EnvironmentRow[]>(() => {
    if (!resources.value) {
      return [];
    }
    return buildEnvironmentRows(
      resources.value,
      applicationStates.value,
      applicationStatesFailed.value,
      resources.value.project.id,
      resources.value.environment.id,
    );
  });

  const visibleRows = computed<EnvironmentRow[]>(() =>
    filterEnvironmentRows(rows.value, tab.value, search.value),
  );

  const counts = computed<Record<EnvironmentResourceTab, number>>(() => {
    const listed = rows.value.filter((row) => !row.preview);
    return {
      all: listed.length,
      applications: listed.filter((row) => row.kind === "application").length,
      services: listed.filter((row) => row.kind === "service").length,
      databases: listed.filter((row) => row.kind === "database").length,
    };
  });

  const canWrite = computed<boolean>(() => projectsStore.canWrite);

  async function reload(): Promise<void> {
    if (environmentId.value === "") {
      return;
    }
    reloadGeneration.bump();
    const token = reloadGeneration.current();
    loading.value = true;
    failure.value = null;
    notFound.value = false;
    try {
      const next = await getEnvironmentResources(
        environmentId.value,
        showPreviews.value,
      );
      if (!reloadGeneration.isCurrent(token)) {
        return;
      }
      const canonical = resolveEnvironmentScope(
        { projectId: next.project.id, environmentId: next.environment.id },
        { projectId: projectId.value, environmentId: environmentId.value },
      );
      if (canonical !== null) {
        resources.value = null;
        await router.replace({
          name: "environment-detail",
          params: { projectId: canonical.projectId, environmentId: canonical.environmentId },
        });
        return;
      }
      resources.value = next;
      void loadSiblings(next.project.id);
    } catch (err) {
      if (!reloadGeneration.isCurrent(token)) {
        return;
      }
      notFound.value = isApiError(err) && err.status === 404;
      failure.value = err;
      resources.value = null;
      return;
    } finally {
      if (reloadGeneration.isCurrent(token)) {
        loading.value = false;
      }
    }
    const applications = resources.value?.applications ?? [];
    if (applications.length === 0) {
      applicationStates.value = {};
      applicationStatesFailed.value = false;
      return;
    }
    try {
      const { states } = await latestDeploymentStates(
        applications.map((application) => application.id),
      );
      if (!reloadGeneration.isCurrent(token)) {
        return;
      }
      const next: Record<string, DeploymentState | null> = {};
      applications.forEach((application, index) => {
        next[application.id] = states[index];
      });
      applicationStates.value = next;
      applicationStatesFailed.value = false;
    } catch {
      if (!reloadGeneration.isCurrent(token)) {
        return;
      }
      applicationStates.value = {};
      applicationStatesFailed.value = true;
    }
  }

  async function loadSiblings(forProjectId: string): Promise<void> {
    try {
      const detail = await getProject(forProjectId);
      siblings.value = detail.environments;
    } catch {
      siblings.value = [];
    }
  }

  watch(showPreviews, () => {
    void reload();
  });

  watch([projectId, environmentId], ([, nextEnvironment], [, previousEnvironment]) => {
    if (nextEnvironment !== previousEnvironment) {
      tab.value = "all";
      search.value = "";
      showPreviews.value = false;
      addOpen.value = false;
    }
    void reload();
  });

  onMounted(() => {
    void reload();
  });

  return {
    projectId,
    environmentId,
    loading,
    error,
    notFound,
    resources,
    siblings,
    tab,
    showPreviews,
    search,
    addOpen,
    importOpen,
    rows,
    visibleRows,
    counts,
    canWrite,
    reload,
  };
}

export type EnvironmentPageState = ReturnType<typeof useEnvironmentPage>;
