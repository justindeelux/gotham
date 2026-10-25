import { useMessage } from "naive-ui";
import { computed, onMounted, ref, toValue, watch } from "vue";
import type { ComputedRef, Ref } from "vue";
import { useRouter } from "vue-router";

import type { Environment } from "@/features/projects/api/projects";
import {
  describeProjectError,
  environmentResourceTotal,
} from "@/features/projects/api/projects";
import { i18n } from "@/shared/i18n";
import {
  isEnvironmentNameValid,
  isProjectNameValid,
} from "@/features/projects/schemas/projects";
import { useNameConflict } from "@/features/projects/composables/useNameConflict";
import type { NameConflictState } from "@/features/projects/composables/useNameConflict";
import { useProjectsStore } from "@/features/projects/stores/projects";

export type ProjectTab = "environments" | "variables";

export interface ProjectPageState {
  tab: Ref<ProjectTab>;
  renameOpen: Ref<boolean>;
  renameName: Ref<string>;
  renameBusy: Ref<boolean>;
  renameError: ComputedRef<string | null>;
  renameConflict: NameConflictState;
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
  openRename(): void;
  handleRename(): Promise<void>;
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

  const renameOpen = ref(false);
  const renameName = ref("");
  const renameBusy = ref(false);
  const renameFailure: Ref<unknown> = ref(null);
  const renameError = computed<string | null>(() =>
    renameFailure.value === null
      ? null
      : describeProjectError(renameFailure.value),
  );
  const renameConflict = useNameConflict();

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

  function openRename(): void {
    renameName.value = projectsStore.detail?.name ?? "";
    renameFailure.value = null;
    renameConflict.clear();
    renameOpen.value = true;
  }

  async function handleRename(): Promise<void> {
    const detail = projectsStore.detail;
    if (renameBusy.value || !detail || !isProjectNameValid(renameName.value)) {
      return;
    }
    const projectId = currentId();
    renameBusy.value = true;
    renameFailure.value = null;
    try {
      await projectsStore.rename(detail.id, { name: renameName.value.trim() });
      message.success(String(i18n.global.t("projects.toast.projectRenamed")));
      if (currentId() === projectId) {
        renameOpen.value = false;
      }
    } catch (error) {
      if (!renameConflict.take(error)) {
        renameFailure.value = error;
      }
    } finally {
      renameBusy.value = false;
    }
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
    renameOpen.value = false;
    renameFailure.value = null;
    renameConflict.clear();
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
    renameOpen,
    renameName,
    renameBusy,
    renameError,
    renameConflict,
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
    openRename,
    handleRename,
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
