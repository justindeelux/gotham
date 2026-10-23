import { useMessage } from "naive-ui";
import { computed, onMounted, ref, toValue, watch } from "vue";
import type { ComputedRef, Ref } from "vue";
import {
  getEnvironmentVariables,
  getProjectVariables,
  replaceEnvironmentVariables,
  replaceProjectVariables,
} from "@/features/projects/api/variables";
import { describeProjectError } from "@/features/projects/api/projects";
import {
  buildVariablesPayload,
  existingSecretKeysOf,
  maxSharedVariables,
  secretWithoutValueKey,
  toVariableDrafts,
  validateVariableDrafts,
} from "@/features/projects/schemas/variables";
import type {
  SharedVariable,
  VariableDraft,
} from "@/features/projects/schemas/variables";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { useTeamsStore } from "@/features/teams";
import { i18n } from "@/shared/i18n";
import { createRequestGeneration } from "@/shared/utils/requestGeneration";

export type SharedVariablesScope =
  | { kind: "project"; projectId: string }
  | { kind: "environment"; environmentId: string };

export function scopeKey(scope: SharedVariablesScope): string {
  return scope.kind === "project"
    ? `project:${scope.projectId}`
    : `environment:${scope.environmentId}`;
}

export interface SharedVariablesState {
  draft: Ref<VariableDraft[]>;
  loading: Ref<boolean>;
  loadError: ComputedRef<string | null>;
  saving: Ref<boolean>;
  saveError: ComputedRef<string | null>;
  loadedFor: Ref<string>;
  saveDisabled: ComputedRef<boolean>;
  problems: ComputedRef<string[]>;
  storedSecrets: Ref<Set<string>>;
  addRow(): void;
  removeRow(_index: number): void;
  updateRow(_index: number, _patch: Partial<VariableDraft>): void;
  load(): Promise<void>;
  save(): Promise<void>;
  retry(): Promise<void>;
}

export function useSharedVariables(
  scopeSource: SharedVariablesScope | Ref<SharedVariablesScope> | (() => SharedVariablesScope),
): SharedVariablesState {
  const message = useMessage();
  const projectsStore = useProjectsStore();
  const teamsStore = useTeamsStore();

  const currentScope = (): SharedVariablesScope => toValue(scopeSource);

  const draft = ref<VariableDraft[]>([]);
  const loading = ref(false);
  const loadFailure: Ref<unknown> = ref(null);
  const loadError = computed<string | null>(() =>
    loadFailure.value === null ? null : describeProjectError(loadFailure.value),
  );
  const saving = ref(false);
  const saveFailure: Ref<unknown> = ref(null);
  const saveError = computed<string | null>(() =>
    saveFailure.value === null ? null : describeProjectError(saveFailure.value),
  );
  const loadedFor = ref("");
  const storedSecrets = ref<Set<string>>(new Set());

  const readGeneration = createRequestGeneration();

  const canWrite = computed<boolean>(() => projectsStore.canWrite);

  const problems = computed<string[]>(() =>
    validateVariableDrafts(draft.value, storedSecrets.value),
  );

  const saveDisabled = computed<boolean>(
    () =>
      !canWrite.value ||
      loading.value ||
      saving.value ||
      loadedFor.value === "" ||
      loadedFor.value !== scopeKey(currentScope()) ||
      problems.value.length > 0,
  );

  async function readScope(scope: SharedVariablesScope): Promise<SharedVariable[]> {
    const teamId = teamsStore.activeTeamId;
    if (scope.kind === "project") {
      return getProjectVariables(teamId, scope.projectId);
    }
    return getEnvironmentVariables(teamId, scope.environmentId);
  }

  async function load(): Promise<void> {
    const scope = currentScope();
    const key = scopeKey(scope);
    if (key.endsWith(":") || key === "project:" || key === "environment:") {
      return;
    }
    const token = readGeneration.current();
    loading.value = true;
    loadFailure.value = null;
    try {
      const variables = await readScope(scope);
      if (!readGeneration.isCurrent(token)) {
        return;
      }
      draft.value = toVariableDrafts(variables);
      storedSecrets.value = existingSecretKeysOf(variables);
      loadedFor.value = key;
    } catch (error) {
      if (!readGeneration.isCurrent(token)) {
        return;
      }
      loadFailure.value = error;
      loadedFor.value = "";
    } finally {
      if (readGeneration.isCurrent(token)) {
        loading.value = false;
      }
    }
  }

  async function save(): Promise<void> {
    const scope = currentScope();
    const key = scopeKey(scope);
    if (saving.value || loadedFor.value === "" || loadedFor.value !== key) {
      return;
    }
    if (problems.value.length > 0) {
      return;
    }
    const teamId = teamsStore.activeTeamId;
    const payload = buildVariablesPayload(draft.value, storedSecrets.value);
    saving.value = true;
    saveFailure.value = null;
    try {
      const saved =
        scope.kind === "project"
          ? await replaceProjectVariables(teamId, scope.projectId, payload)
          : await replaceEnvironmentVariables(teamId, scope.environmentId, payload);
      if (scopeKey(currentScope()) !== key) {
        return;
      }
      draft.value = toVariableDrafts(saved);
      storedSecrets.value = existingSecretKeysOf(saved);
      message.success(String(i18n.global.t("projects.toast.variablesSaved")));
    } catch (error) {
      if (scopeKey(currentScope()) === key) {
        saveFailure.value = error;
        const dropped = secretWithoutValueKey(describeProjectError(error));
        if (dropped !== null) {
          const next = new Set(storedSecrets.value);
          next.delete(dropped);
          storedSecrets.value = next;
        }
      }
    } finally {
      saving.value = false;
    }
  }

  async function retry(): Promise<void> {
    await load();
  }

  function addRow(): void {
    if (draft.value.length >= maxSharedVariables) {
      return;
    }
    draft.value = [...draft.value, { key: "", value: "", secret: false }];
  }

  function removeRow(index: number): void {
    draft.value = draft.value.filter((_, rowIndex) => rowIndex !== index);
  }

  function updateRow(index: number, patch: Partial<VariableDraft>): void {
    draft.value = draft.value.map((row, rowIndex) =>
      rowIndex === index ? { ...row, ...patch } : row,
    );
  }

  watch(
    () => scopeKey(currentScope()),
    (next, previous) => {
      if (next !== previous) {
        readGeneration.bump();
        draft.value = [];
        storedSecrets.value = new Set();
        loadedFor.value = "";
        saveFailure.value = null;
        void load();
      }
    },
  );

  watch(
    () => teamsStore.activeTeamId,
    () => {
      readGeneration.bump();
      draft.value = [];
      storedSecrets.value = new Set();
      loadedFor.value = "";
      saveFailure.value = null;
      void load();
    },
  );

  onMounted(() => {
    void load();
  });

  return {
    draft,
    loading,
    loadError,
    saving,
    saveError,
    loadedFor,
    saveDisabled,
    problems,
    storedSecrets,
    addRow,
    removeRow,
    updateRow,
    load,
    save,
    retry,
  };
}
