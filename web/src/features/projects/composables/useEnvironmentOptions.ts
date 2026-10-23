import { computed, onMounted, ref } from "vue";
import type { Ref } from "vue";

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
export function useEnvironmentOptions() {
  const teamsStore = useTeamsStore();

  const projectsLoading = ref(false);
  const projectsFailure: Ref<unknown> = ref(null);
  const projectsError = computed<string | null>(() =>
    projectsFailure.value === null
      ? null
      : describeProjectError(projectsFailure.value),
  );
  const projects = ref<Project[]>([]);
  const environmentsByProject = ref<Record<string, Environment[]>>({});
  const environmentsLoading = ref(false);
  const environmentsFailure: Ref<unknown> = ref(null);
  const environmentsError = computed<string | null>(() =>
    environmentsFailure.value === null
      ? null
      : describeProjectError(environmentsFailure.value),
  );

  const projectOptions = computed<Array<{ label: string; value: string }>>(() =>
    projects.value.map((project) => ({
      label: project.name,
      value: project.id,
    })),
  );

  const canWrite = computed<boolean>(() => {
    const role = teamsStore.activeTeam?.role ?? null;
    return role === null || role !== "read_only";
  });

  function environmentOptions(projectId: string): Array<{ label: string; value: string }> {
    return (environmentsByProject.value[projectId] ?? []).map((environment) => ({
      label: environment.name,
      value: environment.id,
    }));
  }

  function projectNameOf(projectId: string): string {
    return projects.value.find((project) => project.id === projectId)?.name ?? "";
  }

  function environmentNameOf(projectId: string, environmentId: string): string {
    return (
      environmentsByProject.value[projectId]?.find(
        (environment) => environment.id === environmentId,
      )?.name ?? ""
    );
  }

  async function fetchProjects(): Promise<void> {
    projectsLoading.value = true;
    projectsFailure.value = null;
    try {
      projects.value = await listProjectsRequest(teamsStore.activeTeamId);
    } catch (error) {
      projectsFailure.value = error;
    } finally {
      projectsLoading.value = false;
    }
  }

  async function fetchEnvironments(projectId: string): Promise<void> {
    if (projectId === "" || environmentsByProject.value[projectId]) {
      return;
    }
    environmentsLoading.value = true;
    environmentsFailure.value = null;
    try {
      const detail = await getProjectRequest(teamsStore.activeTeamId, projectId);
      environmentsByProject.value[projectId] = detail.environments;
    } catch (error) {
      environmentsFailure.value = error;
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

export type EnvironmentOptions = ReturnType<typeof useEnvironmentOptions>;
