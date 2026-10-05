import { computed, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";

import type { DeploymentState } from "@/features/applications/api/applications";
import {
  latestDeploymentStates,
} from "@/features/applications/api/applications";
import {
  describeProjectError,
  getEnvironmentResources,
} from "@/features/projects/api/projects";
import type {
  EnvironmentResourceApplication,
  EnvironmentResourceDatabase,
  EnvironmentResources,
} from "@/features/projects/api/projects";
import { serviceStatusTagType } from "@/features/services/api/services";
import type { Service } from "@/features/services/api/services";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { useTeamsStore } from "@/features/teams";

/** Resource kinds shown on the environment page. */
export type EnvironmentResourceKind = "application" | "service" | "database";

/** Type tabs of the unified resource table. */
export type EnvironmentResourceTab = "all" | "applications" | "services" | "databases";

/** One unified table row (every resource links to its nested detail page). */
export interface EnvironmentRow {
  kind: EnvironmentResourceKind;
  id: string;
  name: string;
  subtitle: string;
  serverName: string;
  statusText: string;
  statusTag: "success" | "warning" | "error" | "default";
  to: { name: string; params: Record<string, string> };
}

/**
 * applicationStatusView maps an application's newest deployment state onto
 * the table status. A null state means the application never deployed; the
 * caller passes "unknown" through when the state read itself failed, so a
 * failed read never renders as "not deployed".
 */
export function applicationStatusView(
  state: DeploymentState | null | "unknown",
): { text: string; tag: EnvironmentRow["statusTag"] } {
  if (state === "unknown") {
    return { text: "unknown", tag: "default" };
  }
  if (state === null) {
    return { text: "not deployed", tag: "default" };
  }
  if (state === "running") {
    return { text: "running", tag: "success" };
  }
  if (state === "failed") {
    return { text: "failed", tag: "error" };
  }
  return { text: "deploying", tag: "warning" };
}

/** databaseStatusView maps a database lifecycle status onto the table status. */
export function databaseStatusView(
  status: string,
): { text: string; tag: EnvironmentRow["statusTag"] } {
  if (status === "running") {
    return { text: "running", tag: "success" };
  }
  if (status === "error") {
    return { text: "error", tag: "error" };
  }
  if (status === "creating") {
    return { text: "creating", tag: "warning" };
  }
  return { text: status === "" ? "unknown" : status, tag: "default" };
}

/** applicationSubtitle renders `repo · branch` for the table sub-line. */
export function applicationSubtitle(application: EnvironmentResourceApplication): string {
  const parts = [application.repo, application.branch].filter(
    (part) => part.trim() !== "",
  );
  return parts.length > 0 ? parts.join(" · ") : "—";
}

/** serviceSubtitle renders the routed domains, or the fallback kind label. */
export function serviceSubtitle(service: Service): string {
  if (service.domains.length > 0) {
    return service.domains.map((route) => route.domain).join(", ");
  }
  return "compose service";
}

/** databaseSubtitle renders `engine:version` for the table sub-line. */
export function databaseSubtitle(database: EnvironmentResourceDatabase): string {
  return database.version ? `${database.engine}:${database.version}` : database.engine;
}

/**
 * buildEnvironmentRows flattens one resources envelope into unified table
 * rows. Route params ride each row so the table links straight to the nested
 * detail pages.
 */
export function buildEnvironmentRows(
  resources: EnvironmentResources,
  applicationStates: Record<string, DeploymentState | null>,
  applicationStatesFailed: boolean,
  projectId: string,
  environmentId: string,
): EnvironmentRow[] {
  const rows: EnvironmentRow[] = [];
  for (const application of resources.applications) {
    const state = applicationStates[application.id] ?? null;
    const view = applicationStatusView(
      state === null && applicationStatesFailed ? "unknown" : state,
    );
    rows.push({
      kind: "application",
      id: application.id,
      name: application.name,
      subtitle: applicationSubtitle(application),
      serverName: application.server_name || "unassigned",
      statusText: view.text,
      statusTag: view.tag,
      to: { name: "application-detail", params: { projectId, environmentId, id: application.id } },
    });
  }
  for (const service of resources.services) {
    rows.push({
      kind: "service",
      id: service.id,
      name: service.name,
      subtitle: serviceSubtitle(service),
      serverName: service.server_name,
      statusText: service.status,
      statusTag: serviceStatusTagType(service.status),
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
      serverName: database.server_name,
      statusText: view.text,
      statusTag: view.tag,
      to: { name: "database-detail", params: { projectId, environmentId, id: database.id } },
    });
  }
  return rows;
}

/**
 * filterEnvironmentRows applies the type tab and the free-text search over
 * name, subtitle and node. An empty query matches everything.
 */
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

/**
 * State behind the environment page (`/projects/:projectId/environments/:environmentId`,
 * PE-5 Linear JUS-34): one resources call for the unified table, plus one
 * newest-deployment read per application for the status column (the envelope
 * carries no deploy state). The page itself only handles layout and dialogs.
 */
export function useEnvironmentPage() {
  const route = useRoute();
  const teamsStore = useTeamsStore();
  const projectsStore = useProjectsStore();

  const projectId = computed<string>(() => String(route.params.projectId ?? ""));
  const environmentId = computed<string>(() => String(route.params.environmentId ?? ""));

  const loading = ref(false);
  const error = ref<string | null>(null);
  const resources = ref<EnvironmentResources | null>(null);
  const applicationStates = ref<Record<string, DeploymentState | null>>({});
  const applicationStatesFailed = ref(false);
  const tab = ref<EnvironmentResourceTab>("all");
  const showPreviews = ref(false);
  const search = ref("");
  const addOpen = ref(false);
  const appWizardOpen = ref(false);
  const importOpen = ref(false);
  const dbWizardOpen = ref(false);

  /** rows flattens the envelope into the unified table (unfiltered). */
  const rows = computed<EnvironmentRow[]>(() => {
    if (!resources.value) {
      return [];
    }
    return buildEnvironmentRows(
      resources.value,
      applicationStates.value,
      applicationStatesFailed.value,
      projectId.value,
      environmentId.value,
    );
  });

  /** visibleRows applies the type tab and the search query. */
  const visibleRows = computed<EnvironmentRow[]>(() =>
    filterEnvironmentRows(rows.value, tab.value, search.value),
  );

  /** counts renders the per-tab totals (they follow the preview switch). */
  const counts = computed<Record<EnvironmentResourceTab, number>>(() => ({
    all: rows.value.length,
    applications: rows.value.filter((row) => row.kind === "application").length,
    services: rows.value.filter((row) => row.kind === "service").length,
    databases: rows.value.filter((row) => row.kind === "database").length,
  }));

  /** canWrite follows the contract's roles: viewers read, members write. */
  const canWrite = computed<boolean>(() => projectsStore.canWrite);

  /** reload reads the envelope, then one newest deployment per application. */
  async function reload(): Promise<void> {
    if (environmentId.value === "") {
      return;
    }
    loading.value = true;
    error.value = null;
    try {
      resources.value = await getEnvironmentResources(
        teamsStore.activeTeamId,
        environmentId.value,
        showPreviews.value,
      );
    } catch (err) {
      error.value = describeProjectError(err);
      resources.value = null;
      return;
    } finally {
      loading.value = false;
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
      const next: Record<string, DeploymentState | null> = {};
      applications.forEach((application, index) => {
        next[application.id] = states[index];
      });
      applicationStates.value = next;
      applicationStatesFailed.value = false;
    } catch {
      // The table still renders without deploy states; the status column
      // says so instead of claiming "not deployed".
      applicationStates.value = {};
      applicationStatesFailed.value = true;
    }
  }

  watch(showPreviews, () => {
    void reload();
  });

  watch(environmentId, () => {
    tab.value = "all";
    search.value = "";
    showPreviews.value = false;
    addOpen.value = false;
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
    resources,
    tab,
    showPreviews,
    search,
    addOpen,
    appWizardOpen,
    importOpen,
    dbWizardOpen,
    rows,
    visibleRows,
    counts,
    canWrite,
    reload,
  };
}

/** EnvironmentPageState is the shared shape passed from the page to its panels. */
export type EnvironmentPageState = ReturnType<typeof useEnvironmentPage>;
