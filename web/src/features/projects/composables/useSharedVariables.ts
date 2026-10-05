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
import { createRequestGeneration } from "@/shared/utils/requestGeneration";

/** SharedVariablesScope names the scope one editor instance manages. */
export type SharedVariablesScope =
  | { kind: "project"; projectId: string }
  | { kind: "environment"; environmentId: string };

/** scopeKey renders one scope for load guards ("project:<id>"). */
export function scopeKey(scope: SharedVariablesScope): string {
  return scope.kind === "project"
    ? `project:${scope.projectId}`
    : `environment:${scope.environmentId}`;
}

/**
 * Editor state for one shared-variables scope (PE-6, Linear JUS-35).
 * Created per mount and dropped on unmount: the draft, the submit guard and
 * the request generation belong to this mount only.
 *
 * Secrets stay write-only: loaded secrets draft with an empty value and a
 * masked placeholder, and the save omits the value for a stored secret key
 * (the backend keeps its sealed ciphertext). Saving replaces the whole set,
 * so removing every row clears the scope.
 */
export interface SharedVariablesState {
  draft: Ref<VariableDraft[]>;
  loading: Ref<boolean>;
  loadError: Ref<string | null>;
  saving: Ref<boolean>;
  saveError: Ref<string | null>;
  /** loadedFor names the scope whose variables are actually held. */
  loadedFor: Ref<string>;
  /** saveDisabled gates Save: loading, saving, stale scope, or problems. */
  saveDisabled: ComputedRef<boolean>;
  /** problems lists the client-side draft problems blocking a save. */
  problems: ComputedRef<string[]>;
  /** storedSecrets holds the keys currently stored as secrets. */
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

  /** currentScope reads the scope live (routes reuse pages across ids). */
  const currentScope = (): SharedVariablesScope => toValue(scopeSource);

  const draft = ref<VariableDraft[]>([]);
  const loading = ref(false);
  const loadError = ref<string | null>(null);
  const saving = ref(false);
  const saveError = ref<string | null>(null);
  const loadedFor = ref("");
  const storedSecrets = ref<Set<string>>(new Set());

  // Invalidates in-flight reads when the scope moves on, so a slow response
  // for the previous scope can never overwrite the current draft.
  const readGeneration = createRequestGeneration();

  /** canWrite follows the contract's roles: viewers read, members write. */
  const canWrite = computed<boolean>(() => projectsStore.canWrite);

  /** problems lists the zod-backed draft problems blocking a save. */
  const problems = computed<string[]>(() =>
    validateVariableDrafts(draft.value, storedSecrets.value),
  );

  /**
   * saveDisabled is the single submit guard: an empty or stale draft can
   * never replace unknown server-side data, and a draft with client-side
   * problems never reaches the backend (whose 400 stays the authority).
   * Viewers never save.
   */
  const saveDisabled = computed<boolean>(
    () =>
      !canWrite.value ||
      loading.value ||
      saving.value ||
      loadedFor.value === "" ||
      loadedFor.value !== scopeKey(currentScope()) ||
      problems.value.length > 0,
  );

  /** readScope fetches one scope's masked variables. */
  async function readScope(scope: SharedVariablesScope): Promise<SharedVariable[]> {
    const teamId = teamsStore.activeTeamId;
    if (scope.kind === "project") {
      return getProjectVariables(teamId, scope.projectId);
    }
    return getEnvironmentVariables(teamId, scope.environmentId);
  }

  /** load refreshes the draft shown in the editor. */
  async function load(): Promise<void> {
    const scope = currentScope();
    const key = scopeKey(scope);
    if (key.endsWith(":") || key === "project:" || key === "environment:") {
      return;
    }
    const token = readGeneration.current();
    loading.value = true;
    loadError.value = null;
    try {
      const variables = await readScope(scope);
      if (!readGeneration.isCurrent(token)) {
        return; // superseded by a scope move
      }
      draft.value = toVariableDrafts(variables);
      storedSecrets.value = existingSecretKeysOf(variables);
      loadedFor.value = key;
    } catch (error) {
      if (!readGeneration.isCurrent(token)) {
        return;
      }
      // Never present a failed read as an empty collection: keep the error
      // state and clear loadedFor so Save stays disabled until a successful
      // read. Unknown server state is never overwritten.
      loadError.value = describeProjectError(error);
      loadedFor.value = "";
    } finally {
      if (readGeneration.isCurrent(token)) {
        loading.value = false;
      }
    }
  }

  /** save replaces the whole set with the draft. */
  async function save(): Promise<void> {
    const scope = currentScope();
    const key = scopeKey(scope);
    // Refuse to write a draft that does not belong to the current scope.
    if (saving.value || loadedFor.value === "" || loadedFor.value !== key) {
      return;
    }
    if (problems.value.length > 0) {
      return;
    }
    const teamId = teamsStore.activeTeamId;
    const payload = buildVariablesPayload(draft.value, storedSecrets.value);
    saving.value = true;
    saveError.value = null;
    try {
      const saved =
        scope.kind === "project"
          ? await replaceProjectVariables(teamId, scope.projectId, payload)
          : await replaceEnvironmentVariables(teamId, scope.environmentId, payload);
      if (scopeKey(currentScope()) !== key) {
        return; // the scope moved on while the write was in flight
      }
      draft.value = toVariableDrafts(saved);
      storedSecrets.value = existingSecretKeysOf(saved);
      message.success("Shared variables saved. They apply to the next deploy.");
    } catch (error) {
      if (scopeKey(currentScope()) === key) {
        saveError.value = describeProjectError(error);
        // A `secret "X" has no value` 400 means the stored ciphertext is gone
        // (concurrent delete): stop offering the keep path for that key, so
        // the draft asks for a value instead of retrying the 400 forever.
        const dropped = secretWithoutValueKey(saveError.value);
        if (dropped !== null) {
          const next = new Set(storedSecrets.value);
          next.delete(dropped);
          storedSecrets.value = next;
        }
      }
    } finally {
      // The flag belongs to the finished request, not the current scope: a
      // scope move mid-save must not leave Save disabled on the new scope.
      saving.value = false;
    }
  }

  /** retry reloads after a failed read. */
  async function retry(): Promise<void> {
    await load();
  }

  /** addRow appends an empty row for the next variable. */
  function addRow(): void {
    if (draft.value.length >= maxSharedVariables) {
      return;
    }
    draft.value = [...draft.value, { key: "", value: "", secret: false }];
  }

  /** removeRow drops one row by index. */
  function removeRow(index: number): void {
    draft.value = draft.value.filter((_, rowIndex) => rowIndex !== index);
  }

  /** updateRow replaces one row, keeping the array immutable for v-model. */
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
        saveError.value = null;
        void load();
      }
    },
  );

  watch(
    () => teamsStore.activeTeamId,
    () => {
      // A team switch invalidates the held draft like a scope move does:
      // the old team's rows must never be readable (or writable) under the
      // new selection, and the in-flight read for the old team is dropped.
      readGeneration.bump();
      draft.value = [];
      storedSecrets.value = new Set();
      loadedFor.value = "";
      saveError.value = null;
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
