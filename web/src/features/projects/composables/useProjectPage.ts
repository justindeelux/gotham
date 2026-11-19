import { useMessage } from "naive-ui";
import { computed, onMounted, ref, toValue, watch } from "vue";
import type { ComputedRef, Ref } from "vue";
import { useRouter } from "vue-router";

import type { Environment, Project } from "@/features/projects/api/projects";
import {
  describeProjectError,
  environmentResourceTotal,
} from "@/features/projects/api/projects";
import { i18n } from "@/shared/i18n";
import {
  isEnvironmentNameValid,
  isProjectDescriptionValid,
  isProjectNameValid,
} from "@/features/projects/schemas/projects";
import { useNameConflict } from "@/features/projects/composables/useNameConflict";
import type { NameConflictState } from "@/features/projects/composables/useNameConflict";
import { useProjectsStore } from "@/features/projects/stores/projects";

export type ProjectTab = "environments" | "configuration" | "variables";

export interface ProjectPageState {
  tab: Ref<ProjectTab>;
  configName: Ref<string>;
  configDescription: Ref<string>;
  configBusy: Ref<boolean>;
  configError: ComputedRef<string | null>;
  configConflict: NameConflictState;
  deleteOpen: Ref<boolean>;
  deleting: Ref<boolean>;
  deleteError: ComputedRef<string | null>;
  envCreateOpen: Ref<boolean>;
  envName: Ref<string>;
  envBusy: Ref<boolean>;
  envError: ComputedRef<string | null>;
  envConflict: NameConflictState;
  envRenameTarget: Ref<Environment | null>;
  envRenameName: Ref<string>;
  envRenameBusy: Ref<boolean>;
  envRenameError: ComputedRef<string | null>;
  envRenameConflict: NameConflictState;
  envDeleteTarget: Ref<Environment | null>;
  envDeleting: Ref<boolean>;
  envDeleteError: ComputedRef<string | null>;
  hasResources: ComputedRef<boolean>;
  canWrite: ComputedRef<boolean>;
  handleConfigSave(): Promise<void>;
  applyProject(_project: Project): void;
  handleDelete(): Promise<void>;
  openEnvCreate(): void;
  handleEnvCreate(): Promise<void>;
  openEnvRename(_environment: Environment): void;
  handleEnvRename(): Promise<void>;
  openEnvDelete(_environment: Environment): void;
  handleEnvDelete(): Promise<void>;
  reload(): Promise<void>;
}

export function useProjectPage(
  projectIdSource: string | Ref<string> | ComputedRef<string> | (() => string),
): ProjectPageState {
  const message = useMessage();
  const router = useRouter();
  const projectsStore = useProjectsStore();

  const currentId = (): string => toValue(projectIdSource);

  const tab = ref<ProjectTab>("environments");

  const configName = ref("");
  const configDescription = ref("");
  const configBusy = ref(false);
  const configFailure: Ref<unknown> = ref(null);
  const configError = computed<string | null>(() =>
    configFailure.value === null
      ? null
      : describeProjectError(configFailure.value),
  );
  const configConflict = useNameConflict();

  const deleteOpen = ref(false);
  const deleting = ref(false);
  const deleteFailure: Ref<unknown> = ref(null);
  const deleteError = computed<string | null>(() =>
    deleteFailure.value === null
      ? null
      : describeProjectError(deleteFailure.value),
  );

  const envCreateOpen = ref(false);
  const envName = ref("");
  const envBusy = ref(false);
  const envFailure: Ref<unknown> = ref(null);
  const envError = computed<string | null>(() =>
    envFailure.value === null ? null : describeProjectError(envFailure.value),
  );
  const envConflict = useNameConflict();

  const envRenameTarget = ref<Environment | null>(null);
  const envRenameName = ref("");
  const envRenameBusy = ref(false);
  const envRenameFailure: Ref<unknown> = ref(null);
  const envRenameError = computed<string | null>(() =>
    envRenameFailure.value === null
      ? null
      : describeProjectError(envRenameFailure.value),
  );
  const envRenameConflict = useNameConflict();

  const envDeleteTarget = ref<Environment | null>(null);
  const envDeleting = ref(false);
  const envDeleteFailure: Ref<unknown> = ref(null);
  const envDeleteError = computed<string | null>(() =>
    envDeleteFailure.value === null
      ? null
      : describeProjectError(envDeleteFailure.value),
  );

  const canWrite = computed<boolean>(() => projectsStore.canWrite);

  const hasResources = computed<boolean>(() =>
    projectsStore.environments.some(
      (environment) => environmentResourceTotal(environment) > 0,
    ),
  );

  watch(
    () => projectsStore.detail?.id,
    () => {
      configName.value = projectsStore.detail?.name ?? "";
      configDescription.value = projectsStore.detail?.description ?? "";
    },
    { immediate: true },
  );

  async function handleConfigSave(): Promise<void> {
    const detail = projectsStore.detail;
    if (
      configBusy.value ||
      !detail ||
      !isProjectNameValid(configName.value) ||
      !isProjectDescriptionValid(configDescription.value)
    ) {
      return;
    }
    const projectId = currentId();
    configBusy.value = true;
    configFailure.value = null;
    try {
      const saved = await projectsStore.rename(detail.id, {
        name: configName.value.trim(),
        description: configDescription.value.trim(),
      });
      message.success(String(i18n.global.t("projects.toast.projectUpdated")));
      if (currentId() === projectId) {
        configName.value = saved.name;
        configDescription.value = saved.description;
      }
    } catch (error) {
      if (!configConflict.take(error)) {
        configFailure.value = error;
      }
    } finally {
      configBusy.value = false;
    }
  }

  function applyProject(project: Project): void {
    projectsStore.applyProject(project);
  }

  async function handleDelete(): Promise<void> {
    const detail = projectsStore.detail;
    if (deleting.value || !detail) {
      return;
    }
    const projectId = currentId();
    deleting.value = true;
    deleteFailure.value = null;
    try {
      const name = detail.name;
      await projectsStore.remove(detail.id);
      message.success(
        String(i18n.global.t("projects.toast.deletedProject", { name })),
      );
      if (currentId() !== projectId) {
        return;
      }
      deleteOpen.value = false;
      await router.push({ name: "projects" });
    } catch (error) {
      deleteFailure.value = error;
    } finally {
      deleting.value = false;
    }
  }

  function openEnvCreate(): void {
    envName.value = "";
    envFailure.value = null;
    envConflict.clear();
    envCreateOpen.value = true;
  }

  async function handleEnvCreate(): Promise<void> {
    if (envBusy.value || !isEnvironmentNameValid(envName.value)) {
      return;
    }
    const projectId = currentId();
    envBusy.value = true;
    envFailure.value = null;
    try {
      const environment = await projectsStore.addEnvironment(
        projectId,
        envName.value,
      );
      message.success(
        String(
          i18n.global.t("projects.toast.addedEnvironment", {
            name: environment.name,
          }),
        ),
      );
      if (currentId() === projectId) {
        envCreateOpen.value = false;
      }
    } catch (error) {
      if (!envConflict.take(error)) {
        envFailure.value = error;
      }
    } finally {
      envBusy.value = false;
    }
  }

  function openEnvRename(environment: Environment): void {
    envRenameTarget.value = environment;
    envRenameName.value = environment.name;
    envRenameFailure.value = null;
    envRenameConflict.clear();
  }

  async function handleEnvRename(): Promise<void> {
    const target = envRenameTarget.value;
    if (
      envRenameBusy.value ||
      !target ||
      !isEnvironmentNameValid(envRenameName.value)
    ) {
      return;
    }
    envRenameBusy.value = true;
    envRenameFailure.value = null;
    try {
      await projectsStore.renameEnvironmentEntry(
        target.id,
        envRenameName.value,
      );
      message.success(String(i18n.global.t("projects.toast.environmentRenamed")));
      if (envRenameTarget.value?.id === target.id) {
        envRenameTarget.value = null;
      }
    } catch (error) {
      if (!envRenameConflict.take(error)) {
        envRenameFailure.value = error;
      }
    } finally {
      envRenameBusy.value = false;
    }
  }

  function openEnvDelete(environment: Environment): void {
    envDeleteTarget.value = environment;
    envDeleteFailure.value = null;
  }

  async function handleEnvDelete(): Promise<void> {
    const target = envDeleteTarget.value;
    if (envDeleting.value || !target) {
      return;
    }
    envDeleting.value = true;
    envDeleteFailure.value = null;
    try {
      await projectsStore.removeEnvironment(target.id);
      message.success(
        String(
          i18n.global.t("projects.toast.deletedEnvironment", {
            name: target.name,
          }),
        ),
      );
      if (envDeleteTarget.value?.id === target.id) {
        envDeleteTarget.value = null;
      }
    } catch (error) {
      envDeleteFailure.value = error;
    } finally {
      envDeleting.value = false;
    }
  }

  async function reload(): Promise<void> {
    try {
      await projectsStore.fetchDetail(currentId());
    } catch {
    }
  }

  function resetDialogs(): void {
    configFailure.value = null;
    configConflict.clear();
    deleteOpen.value = false;
    deleteFailure.value = null;
    envCreateOpen.value = false;
    envName.value = "";
    envFailure.value = null;
    envConflict.clear();
    envRenameTarget.value = null;
    envRenameFailure.value = null;
    envRenameConflict.clear();
    envDeleteTarget.value = null;
    envDeleteFailure.value = null;
  }

  watch(currentId, (next, previous) => {
    if (next !== previous) {
      resetDialogs();
      void reload();
    }
  });

  onMounted(async () => {
    await reload();
  });

  return {
    tab,
    configName,
    configDescription,
    configBusy,
    configError,
    configConflict,
    deleteOpen,
    deleting,
    deleteError,
    envCreateOpen,
    envName,
    envBusy,
    envError,
    envConflict,
    envRenameTarget,
    envRenameName,
    envRenameBusy,
    envRenameError,
    envRenameConflict,
    envDeleteTarget,
    envDeleting,
    envDeleteError,
    hasResources,
    canWrite,
    handleConfigSave,
    applyProject,
    handleDelete,
    openEnvCreate,
    handleEnvCreate,
    openEnvRename,
    handleEnvRename,
    openEnvDelete,
    handleEnvDelete,
    reload,
  };
}
