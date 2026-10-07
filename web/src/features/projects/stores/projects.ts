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
/**
 * Projects of the active team, plus the selected project detail.
 *
 * Every read is scoped to the team selected in the teams store (`X-Team-Id`,
 * the personal team when none is selected). Reads are race-guarded by team
 * and by project: a response may only write state while it still belongs to
 * the current selection, and only the newest list read may write at all. A
 * read for another team clears the held list first, so one team's projects
 * can never render under another selection.
 *
 * `canWrite` follows the contract's roles: viewers (read_only) read, members
 * and admins write. While the role is unknown the surface stays writable so
 * an unloaded teams list never hides actions from an admin.
 */
export const useProjectsStore = defineStore("projects", () => {
  const projects = ref<Project[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  /** listFailure retains the raw list refusal; error derives its display. */
  const listFailure: Ref<unknown> = ref(null);
  const error = computed<string | null>(() =>
    listFailure.value === null ? null : describeProjectError(listFailure.value),
  );

  const detail = ref<Project | null>(null);
  const environments = ref<Environment[]>([]);
  const detailLoading = ref(false);
  /** detailFailure retains the raw detail refusal; detailError derives it. */
  const detailFailure: Ref<unknown> = ref(null);
  const detailError = computed<string | null>(() =>
    detailFailure.value === null
      ? null
      : describeProjectError(detailFailure.value),
  );
  /** Project the detail belongs to; empty when no detail is held. */
  const detailProjectId = ref("");

  /** Team the current list belongs to; empty when no list is held. */
  const loadedTeamId = ref("");

  /** Token of the newest list read; a stale response never writes state. */
  let listReadToken = 0;
  /** Token of the newest detail read; a stale response never writes state. */
  let detailReadToken = 0;
  /** Team the current detail belongs to; empty when no detail is held. */
  const detailTeamId = ref("");

  /** activeTeamId reads the current team selection. */
  function activeTeamId(): string {
    return useTeamsStore().activeTeamId;
  }

  /** canWrite reports whether the caller's team role may mutate projects. */
  const canWrite = computed<boolean>(() => {
    const role = useTeamsStore().activeTeam?.role ?? null;
    return role === null || role !== "read_only";
  });

  /** totalResources counts every resource across the listed projects. */
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

  /** clearList drops the held list (used when the team changes). */
  function clearList(): void {
    projects.value = [];
    loaded.value = false;
    listFailure.value = null;
  }

  /** clearDetail drops the held detail (used on team/project change). */
  function clearDetail(): void {
    detail.value = null;
    environments.value = [];
    detailFailure.value = null;
    detailProjectId.value = "";
  }

  /** fetchProjects loads the active team's projects. */
  async function fetchProjects(): Promise<void> {
    const teamId = activeTeamId();
    const token = ++listReadToken;
    const isCurrent = (): boolean =>
      token === listReadToken && activeTeamId() === teamId;

    if (loadedTeamId.value !== teamId) {
      // A different team's rows must never render under this selection.
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
      // The api client already ran the envelope through parseWith.
      projects.value = next;
      loaded.value = true;
    } catch (err) {
      if (!isCurrent()) {
        return;
      }
      listFailure.value = err;
      throw err;
    } finally {
      // Only the newest read owns the spinner; an older one must not clear
      // a loading state the current read still needs — but a read that lost
      // to a team switch (no newer read owns the spinner) still releases
      // it, or the page would spin forever with no retry to run.
      if (token === listReadToken) {
        loading.value = false;
      }
    }
  }

  /** fetchDetail loads one project with its environments. */
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
      // The api client already ran the envelope through parseWith.
      detail.value = next.project;
      environments.value = next.environments;
    } catch (err) {
      if (!isCurrent()) {
        return;
      }
      detailFailure.value = err;
      throw err;
    } finally {
      // Same spinner ownership as the list read: only the newest read
      // clears, unless no newer read exists to own it.
      if (token === detailReadToken) {
        detailLoading.value = false;
      }
    }
  }

  /** create stores a project and prepends it to the list. */
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

  /** rename patches a project and merges it into the list and detail. */
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

  /** remove deletes a project and drops it from the list and detail. */
  async function remove(id: string): Promise<void> {
    await deleteProjectRequest(activeTeamId(), id);
    projects.value = projects.value.filter((item) => item.id !== id);
    if (detail.value?.id === id) {
      clearDetail();
    }
  }

  /** addEnvironment creates an environment and appends it to the detail. */
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

  /** renameEnvironmentEntry renames one environment in the detail. */
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

  /** removeEnvironment deletes one environment from the detail. */
  async function removeEnvironment(id: string): Promise<void> {
    await deleteEnvironmentRequest(activeTeamId(), id);
    environments.value = environments.value.filter((item) => item.id !== id);
  }

  /**
   * reset drops the held lists, so the next sign-in never sees the previous
   * account's projects. Called on sign-out (see the auth store).
   */
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
