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
import { useTeamsStore } from "@/features/teams";

/** Detail tabs: environments now, shared variables as a PE-6 placeholder. */
export type ProjectTab = "environments" | "variables";

/**
 * Detail page state for `/projects/:projectId` (PE-4, Linear JUS-33).
 * Created per mount and dropped on unmount: every dialog draft, open flag
 * and submit guard belongs to this mount only. Data lives in the projects
 * store; this composable owns the UI state above it.
 */
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
  /** True while the project holds any resource (delete explains itself). */
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
  const teamsStore = useTeamsStore();

  /**
   * currentId reads the route param live. The router reuses this page
   * across detail-to-detail navigation, so a snapshotted id would keep
   * fetching and mutating under the previous project.
   */
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

  /**
   * hasResources reports whether any environment holds resources. The delete
   * dialog uses it to explain the block up front; the backend's 409 stays
   * the authority when a deploy lands between the check and the submit.
   */
  const hasResources = computed<boolean>(() =>
    projectsStore.environments.some(
      (environment) => environmentResourceTotal(environment) > 0,
    ),
  );

  /** openRename prefills the rename dialog with the project name. */
  function openRename(): void {
    renameName.value = projectsStore.detail?.name ?? "";
    renameFailure.value = null;
    renameConflict.clear();
    renameOpen.value = true;
  }

  /** handleRename patches the name; the busy flag is the submit guard. */
  async function handleRename(): Promise<void> {
    const detail = projectsStore.detail;
    if (renameBusy.value || !detail || !isProjectNameValid(renameName.value)) {
      return;
    }
    // Identity of the project that started this mutation: a late success
    // must not close a dialog the user opened after moving to another
    // project (see resetDialogs, which drops the old dialogs on the move).
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

  /** handleDelete deletes the project and returns to the list. */
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
      // The backend message is the actionable part: a project that still
      // holds resources is refused with 409 naming the block.
      deleteFailure.value = error;
    } finally {
      deleting.value = false;
    }
  }

  /** openEnvCreate resets the environment draft. */
  function openEnvCreate(): void {
    envName.value = "";
    envFailure.value = null;
    envConflict.clear();
    envCreateOpen.value = true;
  }

  /** handleEnvCreate adds the environment; the busy flag guards submit. */
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

  /** openEnvRename prefills the rename dialog with the environment name. */
  function openEnvRename(environment: Environment): void {
    envRenameTarget.value = environment;
    envRenameName.value = environment.name;
    envRenameFailure.value = null;
    envRenameConflict.clear();
  }

  /** handleEnvRename renames the targeted environment. */
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
      // Same late-response guard, on dialog identity: only the dialog this
      // mutation opened may close (a move already dropped it via
      // resetDialogs, so the check also covers the cross-project case).
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

  /** openEnvDelete targets one environment for the delete confirm. */
  function openEnvDelete(environment: Environment): void {
    envDeleteTarget.value = environment;
    envDeleteFailure.value = null;
  }

  /** handleEnvDelete deletes the targeted environment. */
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

  /** reload refetches the detail (the alert renders the store error). */
  async function reload(): Promise<void> {
    try {
      await projectsStore.fetchDetail(currentId());
    } catch {
      // The store exposes the error; the alert renders it.
    }
  }

  /**
   * resetDialogs drops every draft of the previous project on id change.
   * Busy flags are never cleared here: a request still in flight owns its
   * flag until its own finally runs, or a second submit could start on the
   * newly shown project while the first is unresolved.
   */
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

  watch(
    () => teamsStore.activeTeamId,
    () => {
      void reload();
    },
  );

  watch(currentId, (next, previous) => {
    if (next !== previous) {
      resetDialogs();
      void reload();
    }
  });

  onMounted(async () => {
    try {
      await teamsStore.ensureTeams();
    } catch {
      // The teams surface reports its own failure; the detail still loads.
    }
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
