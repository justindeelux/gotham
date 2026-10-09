import { computed, onMounted, ref, watch } from "vue";
import type { Ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import type { DeploymentState } from "@/features/applications/api/applications";
import {
  latestDeploymentStates,
} from "@/features/applications/api/applications";
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
import { useTeamsStore } from "@/features/teams";
import { createRequestGeneration } from "@/shared/utils/requestGeneration";

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
  /** True for a preview row: rendered nested under its base application. */
  preview: boolean;
  to: { name: string; params: Record<string, string> };
}

/**
 * statusText resolves one environment status key in the active locale.
 * Unknown wire values pass through untouched (technical diagnostics stay
 * raw); English output is unchanged.
 */
function statusText(key: string): string {
  void activeLocale.value;
  return String(i18n.global.t(key));
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

/** databaseStatusView maps a database lifecycle status onto the table status. */
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
  return statusText("projects.environment.composeService");
}

/** databaseSubtitle renders `engine:version` for the table sub-line. */
export function databaseSubtitle(database: EnvironmentResourceDatabase): string {
  return database.version ? `${database.engine}:${database.version}` : database.engine;
}

/**
 * buildEnvironmentRows flattens one resources envelope into unified table
 * rows. Route params ride each row so the table links straight to the nested
 * detail pages. Previews nest directly under their base application (by
 * preview_of); an orphan preview (base outside the envelope) keeps its tag
 * and renders after the bases.
 */
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
      serverName: application.server_name || statusText("projects.environment.unassigned"),
      statusText: view.text,
      statusTag: view.tag,
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
  // Orphan previews (base unknown or outside the envelope) render last.
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
      serverName: database.server_name,
      statusText: view.text,
      statusTag: view.tag,
      preview: false,
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
  const router = useRouter();
  const teamsStore = useTeamsStore();
  const projectsStore = useProjectsStore();

  const projectId = computed<string>(() => String(route.params.projectId ?? ""));
  const environmentId = computed<string>(() => String(route.params.environmentId ?? ""));

  const loading = ref(false);
  /**
   * failure retains the raw resources refusal; error derives its display
   * text reactively so the banner refreshes on a language switch.
   */
  const failure: Ref<unknown> = ref(null);
  const error = computed<string | null>(() =>
    failure.value === null ? null : describeProjectError(failure.value),
  );
  /** notFound renders the 404 state (no retry: the URL names nothing). */
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

  // Invalidates in-flight reloads when the route or the preview switch
  // moves on, so a slow response for the previous environment can never
  // overwrite the current table.
  const reloadGeneration = createRequestGeneration();

  /** rows flattens the envelope into the unified table (unfiltered). */
  const rows = computed<EnvironmentRow[]>(() => {
    if (!resources.value) {
      return [];
    }
    // Row links ride the response ids, never the typed URL: after a
    // canonical redirect the route already matches, and before it the
    // table is empty.
    return buildEnvironmentRows(
      resources.value,
      applicationStates.value,
      applicationStatesFailed.value,
      resources.value.project.id,
      resources.value.environment.id,
    );
  });

  /** visibleRows applies the type tab and the search query. */
  const visibleRows = computed<EnvironmentRow[]>(() =>
    filterEnvironmentRows(rows.value, tab.value, search.value),
  );

  /** counts renders the per-tab totals (previews stay out, like the API counts). */
  const counts = computed<Record<EnvironmentResourceTab, number>>(() => {
    // Previews nest under their base but never inflate the totals.
    const listed = rows.value.filter((row) => !row.preview);
    return {
      all: listed.length,
      applications: listed.filter((row) => row.kind === "application").length,
      services: listed.filter((row) => row.kind === "service").length,
      databases: listed.filter((row) => row.kind === "database").length,
    };
  });

  /** canWrite follows the contract's roles: viewers read, members write. */
  const canWrite = computed<boolean>(() => projectsStore.canWrite);

  /** reload reads the envelope, then one newest deployment per application. */
  async function reload(): Promise<void> {
    if (environmentId.value === "") {
      return;
    }
    // Every reload supersedes the previous one: bump first, then capture.
    reloadGeneration.bump();
    const token = reloadGeneration.current();
    loading.value = true;
    failure.value = null;
    notFound.value = false;
    try {
      const next = await getEnvironmentResources(
        teamsStore.activeTeamId,
        environmentId.value,
        showPreviews.value,
      );
      if (!reloadGeneration.isCurrent(token)) {
        return; // superseded by a route move or preview toggle
      }
      // The response ids are the authority: a wrong project/environment
      // in the URL replaces it with the canonical nested URL instead of
      // rendering silently. The route watcher reloads from there.
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
      // The table still renders without deploy states; the status column
      // says so instead of claiming "not deployed".
      applicationStates.value = {};
      applicationStatesFailed.value = true;
    }
  }

  /**
   * loadSiblings reads the project's environments for the sibling
   * switcher. It never touches the projects store detail, so the
   * ProjectDetailPage state cannot be clobbered from here.
   */
  async function loadSiblings(forProjectId: string): Promise<void> {
    try {
      const detail = await getProject(teamsStore.activeTeamId, forProjectId);
      siblings.value = detail.environments;
    } catch {
      siblings.value = [];
    }
  }

  watch(showPreviews, () => {
    void reload();
  });

  // A canonical redirect can move the project while the environment stays
  // the same id, so both params reload the page.
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

/** EnvironmentPageState is the shared shape passed from the page to its panels. */
export type EnvironmentPageState = ReturnType<typeof useEnvironmentPage>;
