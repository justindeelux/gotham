import { computed, onMounted, ref } from "vue";

import {
  describeProjectError,
  getProject as getProjectRequest,
  listProjects as listProjectsRequest,
} from "@/features/projects/api/projects";
import type {
  Environment,
  Project,
} from "@/features/projects/api/projects";
import { useTeamsStore } from "@/features/teams";

/**
 * Project/environment options behind the create-flow scope summary and the
 * resource move card (PE-5, Linear JUS-34).
 *
 * Page-local (per-mount refs): the projects store's detail is never touched,
 * so a select on a detail page cannot clobber the ProjectDetailPage state.
 * Environments load lazily per project and are cached for the mount.
 */
export function useEnvironmentOptions() {
  const teamsStore = useTeamsStore();

  const projectsLoading = ref(false);
  const projectsError = ref<string | null>(null);
  const projects = ref<Project[]>([]);
  const environmentsByProject = ref<Record<string, Environment[]>>({});
  const environmentsLoading = ref(false);
  const environmentsError = ref<string | null>(null);

  /** projectOptions lists every project of the active team for the selects. */
  const projectOptions = computed<Array<{ label: string; value: string }>>(() =>
    projects.value.map((project) => ({
      label: project.name,
      value: project.id,
    })),
  );

  /** canWrite follows the contract's roles: viewers read, members write. */
  const canWrite = computed<boolean>(() => {
    const role = teamsStore.activeTeam?.role ?? null;
    return role === null || role !== "read_only";
  });

  /** environmentOptions lists the cached environments of one project. */
  function environmentOptions(projectId: string): Array<{ label: string; value: string }> {
    return (environmentsByProject.value[projectId] ?? []).map((environment) => ({
      label: environment.name,
      value: environment.id,
    }));
  }

  /** projectNameOf resolves a project id to its display name. */
  function projectNameOf(projectId: string): string {
    return projects.value.find((project) => project.id === projectId)?.name ?? "";
  }

  /** environmentNameOf resolves an environment id to its display name. */
  function environmentNameOf(projectId: string, environmentId: string): string {
    return (
      environmentsByProject.value[projectId]?.find(
        (environment) => environment.id === environmentId,
      )?.name ?? ""
    );
  }

  /** fetchProjects loads the active team's projects for the selects. */
  async function fetchProjects(): Promise<void> {
    projectsLoading.value = true;
    projectsError.value = null;
    try {
      projects.value = await listProjectsRequest(teamsStore.activeTeamId);
    } catch (error) {
      projectsError.value = describeProjectError(error);
    } finally {
      projectsLoading.value = false;
    }
  }

  /** fetchEnvironments loads (and caches) one project's environments. */
  async function fetchEnvironments(projectId: string): Promise<void> {
    if (projectId === "" || environmentsByProject.value[projectId]) {
      return;
    }
    environmentsLoading.value = true;
    environmentsError.value = null;
    try {
      const detail = await getProjectRequest(teamsStore.activeTeamId, projectId);
      environmentsByProject.value[projectId] = detail.environments;
    } catch (error) {
      environmentsError.value = describeProjectError(error);
    } finally {
      environmentsLoading.value = false;
    }
  }

  onMounted(() => {
    void fetchProjects();
  });

  return {
    projectsLoading,
    projectsError,
    projects,
    environmentsLoading,
    environmentsError,
    projectOptions,
    canWrite,
    environmentOptions,
    projectNameOf,
    environmentNameOf,
    fetchProjects,
    fetchEnvironments,
  };
}

/** EnvironmentOptions is the shared scope-select state. */
export type EnvironmentOptions = ReturnType<typeof useEnvironmentOptions>;
