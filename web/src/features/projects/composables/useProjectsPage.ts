import { useMessage } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import type { ComputedRef, Ref } from "vue";
import { useRouter } from "vue-router";

import { describeProjectError } from "@/features/projects/api/projects";
import { i18n } from "@/shared/i18n";
import { useNameConflict } from "@/features/projects/composables/useNameConflict";
import type { NameConflictState } from "@/features/projects/composables/useNameConflict";
import {
  filterProjects,
  isProjectDescriptionValid,
  isProjectNameValid,
} from "@/features/projects/schemas/projects";
import { useProjectsStore } from "@/features/projects/stores/projects";

export interface ProjectsPageState {
  query: Ref<string>;
  filtered: ComputedRef<import("@/features/projects/api/projects").Project[]>;
  createOpen: Ref<boolean>;
  createName: Ref<string>;
  createDescription: Ref<string>;
  createBusy: Ref<boolean>;
  createError: ComputedRef<string | null>;
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

  const query = ref("");
  const filtered = computed(() =>
    filterProjects(projectsStore.projects, query.value),
  );

  const createOpen = ref(false);
  const createName = ref("");
  const createDescription = ref("");
  const createBusy = ref(false);
  const createFailure: Ref<unknown> = ref(null);
  const createError = computed<string | null>(() =>
    createFailure.value === null
      ? null
      : describeProjectError(createFailure.value),
  );
  const createConflict = useNameConflict();

  const canCreate = computed<boolean>(() => projectsStore.canWrite);

  function openCreate(): void {
    createName.value = "";
    createDescription.value = "";
    createFailure.value = null;
    createConflict.clear();
    createOpen.value = true;
  }

  async function handleCreate(): Promise<void> {
    if (
      createBusy.value ||
      !isProjectNameValid(createName.value) ||
      !isProjectDescriptionValid(createDescription.value)
    ) {
      return;
    }
    createBusy.value = true;
    createFailure.value = null;
    try {
      const project = await projectsStore.create({
        name: createName.value,
        description: createDescription.value,
      });
      message.success(
        String(
          i18n.global.t("projects.toast.createdProject", { name: project.name }),
        ),
      );
      createOpen.value = false;
      await router.push({ name: "project-detail", params: { projectId: project.id } });
    } catch (error) {
      if (!createConflict.take(error)) {
        createFailure.value = error;
      }
    } finally {
      createBusy.value = false;
    }
  }

  async function reload(): Promise<void> {
    try {
      await projectsStore.fetchProjects();
    } catch {
    }
  }

  onMounted(async () => {
    await reload();
  });

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
