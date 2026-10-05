import { useMessage } from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import type { ComputedRef, Ref } from "vue";
import { useRouter } from "vue-router";

import { describeProjectError } from "@/features/projects/api/projects";
import { useNameConflict } from "@/features/projects/composables/useNameConflict";
import type { NameConflictState } from "@/features/projects/composables/useNameConflict";
import {
  filterProjects,
  isProjectDescriptionValid,
  isProjectNameValid,
} from "@/features/projects/schemas/projects";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { useTeamsStore } from "@/features/teams";

/**
 * List page state for `/projects` (PE-4, Linear JUS-33). Created per mount
 * and dropped on unmount: the search query, the create dialog draft and the
 * submit guard never survive a route change. Data lives in the projects
 * store; this composable owns the UI state above it.
 */
export interface ProjectsPageState {
  query: Ref<string>;
  filtered: ComputedRef<import("@/features/projects/api/projects").Project[]>;
  createOpen: Ref<boolean>;
  createName: Ref<string>;
  createDescription: Ref<string>;
  createBusy: Ref<boolean>;
  createError: Ref<string | null>;
  createConflict: NameConflictState;
  canCreate: ComputedRef<boolean>;
  openCreate(): void;
  handleCreate(): Promise<void>;
  reload(): Promise<void>;
}

export function useProjectsPage(): ProjectsPageState {
  const message = useMessage();
  const router = useRouter();
  const projectsStore = useProjectsStore();
  const teamsStore = useTeamsStore();

  const query = ref("");
  const filtered = computed(() =>
    filterProjects(projectsStore.projects, query.value),
  );

  const createOpen = ref(false);
  const createName = ref("");
  const createDescription = ref("");
  const createBusy = ref(false);
  const createError = ref<string | null>(null);
  /** createConflict shows a 409 name-taken message on the name field. */
  const createConflict = useNameConflict();

  /** canCreate gates the create dialog on the writer role (viewers read). */
  const canCreate = computed<boolean>(() => projectsStore.canWrite);

  /** openCreate resets the create dialog draft. */
  function openCreate(): void {
    createName.value = "";
    createDescription.value = "";
    createError.value = null;
    createConflict.clear();
    createOpen.value = true;
  }

  /**
   * handleCreate stores the project and opens its detail page. The busy
   * flag is the single submit guard: a second submit while one is in flight
   * is ignored, so double-clicks and Enter-key repeats create one project.
   */
  async function handleCreate(): Promise<void> {
    if (
      createBusy.value ||
      !isProjectNameValid(createName.value) ||
      !isProjectDescriptionValid(createDescription.value)
    ) {
      return;
    }
    createBusy.value = true;
    createError.value = null;
    try {
      const project = await projectsStore.create({
        name: createName.value,
        description: createDescription.value,
      });
      message.success(`Created project ${project.name}`);
      createOpen.value = false;
      await router.push({ name: "project-detail", params: { projectId: project.id } });
    } catch (error) {
      // A taken name renders inline on the field; the rest stays in the
      // dialog alert.
      if (!createConflict.take(error)) {
        createError.value = describeProjectError(error);
      }
    } finally {
      createBusy.value = false;
    }
  }

  /** reload refetches the list (the alert renders the store error). */
  async function reload(): Promise<void> {
    try {
      await projectsStore.fetchProjects();
    } catch {
      // The store exposes the error; the alert renders it.
    }
  }

  onMounted(async () => {
    try {
      await teamsStore.ensureTeams();
    } catch {
      // The teams surface reports its own failure; projects still load.
    }
    // Always refetch: the held list may belong to another team when the
    // selection switched while this page was unmounted (its watcher was
    // torn down), and a teamless first read may have gone stale.
    await reload();
  });

  watch(
    () => teamsStore.activeTeamId,
    () => {
      // ensureTeams can return early while another surface loads the teams,
      // leaving the first read teamless and stale; the switch to the real
      // selection refetches here so the page always converges.
      void reload();
    },
  );

  return {
    query,
    filtered,
    createOpen,
    createName,
    createDescription,
    createBusy,
    createError,
    createConflict,
    canCreate,
    openCreate,
    handleCreate,
    reload,
  };
}
