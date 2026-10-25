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

  let listReadToken = 0;
  let detailReadToken = 0;

  const canWrite = computed<boolean>(() => true);

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
    const token = ++listReadToken;
    const isCurrent = (): boolean => token === listReadToken;

    loading.value = true;
    listFailure.value = null;
    try {
      const next = await listProjectsRequest();
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
    const token = ++detailReadToken;
    const isCurrent = (): boolean =>
      token === detailReadToken && detailProjectId.value === projectId;

    if (detailProjectId.value !== projectId) {
      clearDetail();
      detailProjectId.value = projectId;
    }
    detailLoading.value = true;
    detailFailure.value = null;
    try {
      const next = await getProjectRequest(projectId);
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
    const created = await createProjectRequest({
      name: input.name.trim(),
      ...(input.description?.trim()
        ? { description: input.description.trim() }
        : {}),
    });
    projects.value = [created.project, ...projects.value];
    loaded.value = true;
    return created.project;
  }

  async function rename(id: string, input: UpdateProjectInput): Promise<Project> {
    const project = await renameProjectRequest(id, input);
    projects.value = projects.value.map((item) =>
      item.id === id ? project : item,
    );
    if (detail.value?.id === id) {
      detail.value = project;
    }
    return project;
  }

  async function remove(id: string): Promise<void> {
    await deleteProjectRequest(id);
    projects.value = projects.value.filter((item) => item.id !== id);
    if (detail.value?.id === id) {
      clearDetail();
    }
  }

  async function addEnvironment(
    projectId: string,
    name: string,
  ): Promise<Environment> {
    const environment = await createEnvironmentRequest(projectId, {
      name: name.trim(),
    });
    if (detailProjectId.value === projectId) {
      environments.value = [...environments.value, environment];
    }
    return environment;
  }

  async function renameEnvironmentEntry(
    id: string,
    name: string,
  ): Promise<Environment> {
    const environment = await renameEnvironmentRequest(id, {
      name: name.trim(),
    });
    environments.value = environments.value.map((item) =>
      item.id === id ? environment : item,
    );
    return environment;
  }

  async function removeEnvironment(id: string): Promise<void> {
    await deleteEnvironmentRequest(id);
    environments.value = environments.value.filter((item) => item.id !== id);
  }

  function reset(): void {
    listReadToken += 1;
    detailReadToken += 1;
    clearList();
    clearDetail();
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
