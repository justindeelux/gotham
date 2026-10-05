<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NIcon,
  NInput,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  NText,
} from "naive-ui";
import { computed } from "vue";

import {
  inheritedOriginLabel,
  isOverriddenBy,
  isSharedVariableKeyValid,
  isSharedVariableValueValid,
  maxSharedVariables,
  rowKeyFromServerError,
} from "@/features/projects/schemas/variables";
import type {
  InheritedVariable,
  VariableDraft,
} from "@/features/projects/schemas/variables";
import { useStableRowKeys } from "@/shared/composables/useStableRowKeys";
import GothamIcon from "@/shared/ui/GothamIcon.vue";

const props = withDefaults(
  defineProps<{
    draft: VariableDraft[];
    loading: boolean;
    loadError: string | null;
    saveError: string | null;
    saving: boolean;
    saveDisabled: boolean;
    canWrite: boolean;
    /** storedSecrets holds the keys currently stored as secrets (keep hint). */
    storedSecrets?: string[];
    /**
     * problems lists the client-side draft problems blocking a save
     * (duplicates, cap, secret without value). Rendered as a summary so Save
     * is never disabled without an explanation.
     */
    problems?: string[];
    /** inherited renders read-only context rows above the editor, if any. */
    inherited?: InheritedVariable[];
    inheritedLoading?: boolean;
    cardTitle?: string;
    precedenceHint?: string;
  }>(),
  {
    storedSecrets: () => [],
    problems: () => [],
    inherited: () => [],
    inheritedLoading: false,
    cardTitle: "Shared variables",
    precedenceHint:
      "Application variables override environment ones; environment ones override project ones.",
  },
);

const emit = defineEmits<{
  "update:draft": [value: VariableDraft[]];
  save: [];
  retry: [];
}>();

/**
 * Shared-variables editor (PE-6, Linear JUS-35), used by the project tab, the
 * environment page section and (read-only) the application env tab context.
 * Secrets are write-only: a stored secret drafts with an empty value and a
 * masked placeholder, and saving it untouched keeps the sealed ciphertext.
 */

const rows = computed<VariableDraft[]>(() => props.draft);
const { keys: rowKeys, insertAt, removeAt } = useStableRowKeys(() => rows.value.length);
const storedSet = computed<Set<string>>(() => new Set(props.storedSecrets));

/** isKeptSecret marks a stored secret the save would keep (empty value). */
function isKeptSecret(row: VariableDraft): boolean {
  return row.secret && row.value === "" && storedSet.value.has(row.key);
}

/** keyStatus flags a row key against the contract rule (display-only). */
function keyStatus(key: string): "error" | undefined {
  return key !== "" && !isSharedVariableKeyValid(key) ? "error" : undefined;
}

/**
 * duplicateKeyAt marks every occurrence after the first: the first row owns
 * the key, each later twin renders "Duplicate key." inline.
 */
function duplicateKeyAt(index: number): boolean {
  const key = rows.value[index]?.key;
  return (
    key !== undefined &&
    key !== "" &&
    rows.value.findIndex((row) => row.key === key) !== index
  );
}

/** rowProblem names one row's client-side problem for inline display. */
function rowProblem(row: VariableDraft, index: number): string | null {
  if (row.key === "") {
    return "Key is required.";
  }
  if (!isSharedVariableKeyValid(row.key)) {
    return "Must match ^[A-Za-z_][A-Za-z0-9_]*$, 128 characters or fewer.";
  }
  if (duplicateKeyAt(index)) {
    return "Duplicate key.";
  }
  if (row.value !== "" && !isSharedVariableValueValid(row.value)) {
    return "Value must not contain NUL.";
  }
  if (row.secret && row.value === "" && !storedSet.value.has(row.key)) {
    return "A new secret needs a value.";
  }
  if (!row.secret && row.value === "" && storedSet.value.has(row.key)) {
    return "Saving clears the stored secret — enter a value.";
  }
  return null;
}

/**
 * serverRowKey extracts the row key a backend 400 names (if any), so the
 * refusal renders inline on the matching row as well as in the alert.
 */
const serverRowKey = computed<string | null>(() =>
  props.saveError ? rowKeyFromServerError(props.saveError) : null,
);

/**
 * keyInputProps names the native key input for assistive tech (the NInput
 * `aria-label` prop lands on the wrapper, not the `<input>`) and opts it
 * out of password-manager autofill.
 */
const keyInputProps = {
  "aria-label": "Variable name",
  autocomplete: "off",
} as const;

/**
 * valueInputProps names the native value input after its key (so rows are
 * distinguishable) and steers password managers: secrets hint
 * `new-password`, plain values opt out.
 */
function valueInputProps(row: VariableDraft): Record<string, string> {
  const name =
    row.key !== ""
      ? row.secret
        ? `Secret value for ${row.key}`
        : `Value for ${row.key}`
      : "Variable value";
  return {
    "aria-label": name,
    autocomplete: row.secret ? "new-password" : "off",
  };
}

/** secretSwitchLabel names the secret toggle after its row key. */
function secretSwitchLabel(row: VariableDraft): string {
  return row.key !== "" ? `Secret for ${row.key}` : "Secret variable";
}

/** updateRow replaces one row, keeping the array immutable for v-model. */
function updateRow(index: number, patch: Partial<VariableDraft>): void {
  emit(
    "update:draft",
    rows.value.map((row, rowIndex) =>
      rowIndex === index ? { ...row, ...patch } : row,
    ),
  );
}

/** addRow appends an empty row for the next variable. */
function addRow(): void {
  if (rows.value.length >= maxSharedVariables) {
    return;
  }
  insertAt(rows.value.length);
  emit("update:draft", [...rows.value, { key: "", value: "", secret: false }]);
}

/** removeRow drops one row by index. */
function removeRow(index: number): void {
  removeAt(index);
  emit(
    "update:draft",
    rows.value.filter((_, rowIndex) => rowIndex !== index),
  );
}

/** inheritedShown renders only once its scope finished loading. */
const inheritedShown = computed<InheritedVariable[]>(() =>
  props.inheritedLoading ? [] : props.inherited,
);
</script>

<template>
  <NCard :title="props.cardTitle" class="variables-card">
    <template v-if="props.canWrite" #header-extra>
      <NButton
        type="primary"
        size="small"
        :loading="props.saving"
        :disabled="props.saveDisabled"
        @click="emit('save')"
      >
        Save
      </NButton>
    </template>

    <NSpace vertical :size="12">
      <NText depth="3">{{ props.precedenceHint }}</NText>

      <NAlert v-if="props.loadError" type="error" :show-icon="true">
        <NSpace align="center" :size="12" wrap>
          <span>{{ props.loadError }}</span>
          <NButton size="small" @click="emit('retry')">Retry</NButton>
        </NSpace>
      </NAlert>

      <NSpin :show="props.loading">
        <div v-if="inheritedShown.length > 0" class="inherited">
          <NText depth="3" class="inherited-title">Inherited (read-only)</NText>
          <div class="table-wrap">
            <table class="variables-table">
              <thead>
                <tr>
                  <th scope="col">Key</th>
                  <th scope="col">Value</th>
                  <th scope="col">Origin</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in inheritedShown" :key="`${row.origin}:${row.key}`">
                  <td class="mono" data-label="Key">
                    {{ row.key }}
                    <NTag v-if="row.secret" size="small">secret</NTag>
                  </td>
                  <td class="mono muted" data-label="Value">
                    {{ row.secret ? "••••••••" : row.value }}
                  </td>
                  <td data-label="Origin">
                    <NSpace :size="4" align="center" wrap>
                      <NTag size="small">{{ inheritedOriginLabel(row.origin) }}</NTag>
                      <NTag v-if="isOverriddenBy(row.key, rows)" size="small" type="warning">
                        overridden
                      </NTag>
                    </NSpace>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div v-if="!props.canWrite" class="table-wrap">
          <table class="variables-table">
            <thead>
              <tr>
                <th scope="col">Key</th>
                <th scope="col">Value</th>
                <th scope="col"><span class="sr-only">Flags</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, index) in rows" :key="rowKeys[index] ?? index">
                <td class="mono" data-label="Key">{{ row.key }}</td>
                <td class="mono muted" data-label="Value">
                  {{ row.secret ? "••••••••" : row.value }}
                </td>
                <td data-label="Flags">
                  <NTag v-if="row.secret" size="small">secret</NTag>
                </td>
              </tr>
            </tbody>
          </table>
          <NText v-if="rows.length === 0" depth="3">No variables yet.</NText>
        </div>

        <div v-else class="editor">
          <div v-if="rows.length === 0" class="editor-empty">
            <NText depth="3">No variables yet. Add the first one below.</NText>
          </div>
          <div
            v-for="(row, index) in rows"
            :key="rowKeys[index] ?? index"
            class="editor-row"
          >
            <NInput
              :value="row.key"
              class="mono"
              placeholder="LOG_LEVEL"
              :input-props="keyInputProps"
              :status="keyStatus(row.key)"
              :disabled="props.saving"
              @update:value="(value: string) => updateRow(index, { key: value })"
            />
            <NInput
              :value="row.value"
              class="mono"
              :type="row.secret ? 'password' : 'text'"
              :placeholder="
                row.secret && storedSet.has(row.key)
                  ? '•••••••• (stored — kept)'
                  : 'value'
              "
              :input-props="valueInputProps(row)"
              :status="row.value !== '' && !isSharedVariableValueValid(row.value) ? 'error' : undefined"
              :disabled="props.saving"
              @update:value="(value: string) => updateRow(index, { value })"
            />
            <label class="secret-switch">
              <NSwitch
                :value="row.secret"
                size="small"
                :aria-label="secretSwitchLabel(row)"
                :disabled="props.saving"
                @update:value="(value: boolean) => updateRow(index, { secret: value })"
              />
              <NText depth="3">Secret</NText>
            </label>
            <NButton
              quaternary
              type="error"
              aria-label="Remove variable"
              :disabled="props.saving"
              @click="removeRow(index)"
            >
              <template #icon>
                <NIcon>
                  <GothamIcon name="trash" />
                </NIcon>
              </template>
            </NButton>
            <span v-if="isKeptSecret(row)" class="kept-badge">stored — kept on save</span>
            <span
              v-else-if="serverRowKey !== null && serverRowKey === row.key"
              class="row-error"
            >
              {{ props.saveError }}
            </span>
            <span v-else-if="rowProblem(row, index) !== null" class="row-error">
              {{ rowProblem(row, index) }}
            </span>
          </div>
          <NAlert
            v-if="props.problems.length > 0"
            type="warning"
            :show-icon="true"
            class="problems"
          >
            <NSpace vertical :size="4">
              <span>Fix these before saving:</span>
              <ul class="problems-list">
                <li v-for="problem in props.problems" :key="problem">
                  {{ problem }}
                </li>
              </ul>
            </NSpace>
          </NAlert>
          <NButton
            secondary
            size="small"
            :disabled="props.saving || rows.length >= maxSharedVariables"
            @click="addRow"
          >
            Add variable
          </NButton>
        </div>
      </NSpin>

      <NAlert v-if="props.saveError" type="error" :show-icon="true">
        {{ props.saveError }}
      </NAlert>
    </NSpace>

    <template v-if="props.canWrite" #footer>
      <NText depth="3">
        Saving replaces the whole set; removing every row clears it. Secrets
        are write-only and never shown again. New variables apply to the next
        deploy.
      </NText>
    </template>
  </NCard>
</template>

<style scoped>
.variables-card {
  container-type: inline-size;
}

.table-wrap {
  overflow-x: auto;
}

.variables-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--text-sm);
}

.variables-table th,
.variables-table td {
  text-align: left;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-soft);
  max-width: 240px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.variables-table th {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--muted);
}

.inherited {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin-bottom: var(--space-3);
}

.inherited-title {
  font-weight: 600;
}

.editor {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  align-items: flex-start;
  container-type: inline-size;
}

.editor-empty {
  margin-bottom: var(--space-1);
}

.editor-row {
  display: grid;
  grid-template-columns: minmax(140px, 220px) minmax(0, 1fr) auto auto;
  gap: var(--space-2);
  align-items: center;
  width: 100%;
  min-width: 0;
}

.secret-switch {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  cursor: pointer;
  white-space: nowrap;
}

.kept-badge {
  grid-column: 1 / -1;
  justify-self: start;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--warn-ink);
  background: var(--warn-soft);
  border-radius: var(--radius-pill);
  padding: 2px 8px;
  white-space: nowrap;
}

.row-error {
  grid-column: 1 / -1;
  justify-self: start;
  font-size: var(--text-xs);
  color: var(--danger);
}

.problems {
  width: 100%;
}

.problems-list {
  margin: 0;
  padding-left: var(--space-4);
  font-size: var(--text-xs);
}

.mono {
  font-family: var(--font-mono);
}

.muted {
  color: var(--muted);
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
}

@container (max-width: 560px) {
  .variables-table thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
  }

  .variables-table,
  .variables-table tbody,
  .variables-table tr,
  .variables-table td {
    display: block;
    width: auto;
    max-width: none;
  }

  .variables-table tr {
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    margin-bottom: var(--space-3);
  }

  .variables-table td {
    border-bottom: 0;
    padding: 4px 0;
    white-space: normal;
    overflow: visible;
    text-overflow: clip;
  }

  .editor-row {
    grid-template-columns: minmax(0, 1fr);
  }

  .editor-row > .n-button {
    justify-self: start;
  }

  .secret-switch {
    justify-self: start;
  }
}
</style>
