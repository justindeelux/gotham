import { defineStore } from "pinia";
import { computed, ref } from "vue";
import type { Ref } from "vue";

import {
  createEnvironment as createEnvironmentRequest,
  createProject as createProjectRequest,
  deleteEnvironment as deleteEnvironmentRequest,
  deleteProject as deleteProjectRequest,
  describeProjectError,
  getProject as getProjectRequest,
  listProjects as listProjectsRequest,
  renameEnvironment as renameEnvironmentRequest,
  renameProject as renameProjectRequest,
} from "@/features/projects/api/projects";
import type {
  CreateProjectInput,
  Environment,
  Project,
  UpdateProjectInput,
} from "@/features/projects/api/projects";
import { useTeamsStore } from "@/features/teams";
export const useProjectsStore = defineStore("projects", () => {
  const projects = ref<Project[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const listFailure: Ref<unknown> = ref(null);
  const error = computed<string | null>(() =>
    listFailure.value === null ? null : describeProjectError(listFailure.value),
  );

  const detail = ref<Project | null>(null);
  const environments = ref<Environment[]>([]);
  const detailLoading = ref(false);
  const detailFailure: Ref<unknown> = ref(null);
  const detailError = computed<string | null>(() =>
    detailFailure.value === null
      ? null
      : describeProjectError(detailFailure.value),
  );
  const detailProjectId = ref("");

  const loadedTeamId = ref("");

  let listReadToken = 0;
  let detailReadToken = 0;
  const detailTeamId = ref("");

  function activeTeamId(): string {
    return useTeamsStore().activeTeamId;
  }

  const canWrite = computed<boolean>(() => {
    const role = useTeamsStore().activeTeam?.role ?? null;
    return role === null || role !== "read_only";
  });

  const totalResources = computed<number>(() =>
    projects.value.reduce(
      (sum, project) =>
        sum +
        project.resource_counts.applications +
        project.resource_counts.services +
        project.resource_counts.databases,
      0,
    ),
  );

  function clearList(): void {
    projects.value = [];
    loaded.value = false;
    listFailure.value = null;
  }

  function clearDetail(): void {
    detail.value = null;
    environments.value = [];
    detailFailure.value = null;
    detailProjectId.value = "";
  }

  async function fetchProjects(): Promise<void> {
    const teamId = activeTeamId();
    const token = ++listReadToken;
    const isCurrent = (): boolean =>
      token === listReadToken && activeTeamId() === teamId;

    if (loadedTeamId.value !== teamId) {
      clearList();
      loadedTeamId.value = teamId;
    }
    loading.value = true;
    listFailure.value = null;
    try {
      const next = await listProjectsRequest(teamId);
      if (!isCurrent()) {
        return;
      }
      projects.value = next;
      loaded.value = true;
    } catch (err) {
      if (!isCurrent()) {
        return;
      }
      listFailure.value = err;
      throw err;
    } finally {
      if (token === listReadToken) {
        loading.value = false;
      }
    }
  }

  async function fetchDetail(projectId: string): Promise<void> {
    const teamId = activeTeamId();
    const token = ++detailReadToken;
    const isCurrent = (): boolean =>
      token === detailReadToken &&
      activeTeamId() === teamId &&
      detailProjectId.value === projectId;

    if (detailProjectId.value !== projectId || detailTeamId.value !== teamId) {
      clearDetail();
      detailProjectId.value = projectId;
      detailTeamId.value = teamId;
    }
    detailLoading.value = true;
    detailFailure.value = null;
    try {
      const next = await getProjectRequest(teamId, projectId);
      if (!isCurrent()) {
        return;
      }
      detail.value = next.project;
      environments.value = next.environments;
    } catch (err) {
      if (!isCurrent()) {
        return;
      }
      detailFailure.value = err;
      throw err;
    } finally {
      if (token === detailReadToken) {
        detailLoading.value = false;
      }
    }
  }

  async function create(input: CreateProjectInput): Promise<Project> {
    const teamId = activeTeamId();
    const created = await createProjectRequest(teamId, {
      name: input.name.trim(),
      ...(input.description?.trim()
        ? { description: input.description.trim() }
        : {}),
    });
    if (activeTeamId() !== teamId) {
      return created.project;
    }
    projects.value = [created.project, ...projects.value];
    loaded.value = true;
    return created.project;
  }

  async function rename(id: string, input: UpdateProjectInput): Promise<Project> {
    const project = await renameProjectRequest(activeTeamId(), id, input);
    projects.value = projects.value.map((item) =>
      item.id === id ? project : item,
    );
    if (detail.value?.id === id) {
      detail.value = project;
    }
    return project;
  }

  async function remove(id: string): Promise<void> {
    await deleteProjectRequest(activeTeamId(), id);
    projects.value = projects.value.filter((item) => item.id !== id);
    if (detail.value?.id === id) {
      clearDetail();
    }
  }

  async function addEnvironment(
    projectId: string,
    name: string,
  ): Promise<Environment> {
    const teamId = activeTeamId();
    const environment = await createEnvironmentRequest(teamId, projectId, {
      name: name.trim(),
    });
    if (detailProjectId.value === projectId && activeTeamId() === teamId) {
      environments.value = [...environments.value, environment];
    }
    return environment;
  }

  async function renameEnvironmentEntry(
    id: string,
    name: string,
  ): Promise<Environment> {
    const environment = await renameEnvironmentRequest(activeTeamId(), id, {
      name: name.trim(),
    });
    environments.value = environments.value.map((item) =>
      item.id === id ? environment : item,
    );
    return environment;
  }

  async function removeEnvironment(id: string): Promise<void> {
    await deleteEnvironmentRequest(activeTeamId(), id);
    environments.value = environments.value.filter((item) => item.id !== id);
  }

  function reset(): void {
    listReadToken += 1;
    detailReadToken += 1;
    clearList();
    clearDetail();
    loadedTeamId.value = "";
    detailTeamId.value = "";
    loading.value = false;
    detailLoading.value = false;
  }

  return {
    projects,
    loading,
    loaded,
    error,
    detail,
    environments,
    detailLoading,
    detailError,
    canWrite,
    totalResources,
    fetchProjects,
    fetchDetail,
    create,
    rename,
    remove,
    addEnvironment,
    renameEnvironmentEntry,
    removeEnvironment,
    reset,
  };
});
