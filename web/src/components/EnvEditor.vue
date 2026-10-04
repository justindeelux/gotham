<script setup lang="ts">
import { NButton, NIcon, NInput, NText } from "naive-ui";
import { computed } from "vue";

import type { EnvVar } from "../api/applications";
import { useStableRowKeys } from "../composables/useStableRowKeys";
import { isSecretValue } from "../utils/envSecret";
import GothamIcon from "./GothamIcon.vue";

/**
 * Key/value editor for the plain environment variables sent to the container.
 * Values carrying the `secret:` prefix name a sealed secret (stored AES-256-GCM
 * in the `secrets` table) and are never returned by the API.
 */

interface Props {
  modelValue: EnvVar[];
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:modelValue": [value: EnvVar[]];
}>();

const KEY_PATTERN = /^[A-Z][A-Z0-9_]*$/;

const rows = computed<EnvVar[]>(() => props.modelValue);
const { keys: rowKeys, insertAt, removeAt } = useStableRowKeys(() => rows.value.length);

/** isValidKey enforces the backend-adjacent KEY shape ^[A-Z][A-Z0-9_]*$. */
function isValidKey(key: string): boolean {
  return KEY_PATTERN.test(key.trim());
}

/** updateRow replaces one row, keeping the array immutable for v-model. */
function updateRow(index: number, patch: Partial<EnvVar>): void {
  const next = rows.value.map((row, rowIndex) =>
    rowIndex === index ? { ...row, ...patch } : row,
  );
  emit("update:modelValue", next);
}

/** addRow appends an empty row for the next variable. */
function addRow(): void {
  insertAt(rows.value.length);
  emit("update:modelValue", [...rows.value, { key: "", value: "" }]);
}

/** removeRow drops one row by index. */
function removeRow(index: number): void {
  removeAt(index);
  emit("update:modelValue", rows.value.filter((_, rowIndex) => rowIndex !== index));
}
</script>

<template>
  <div class="env-editor">
    <div v-if="rows.length === 0" class="env-editor__empty">
      <NText depth="3">No environment variables yet. Add the first one below.</NText>
    </div>
    <div
      v-for="(row, index) in rows"
      :key="rowKeys[index] ?? index"
      class="env-editor__row"
    >
      <NInput
        :value="row.key"
        class="mono"
        placeholder="NODE_ENV"
        aria-label="Variable name"
        :status="row.key !== '' && !isValidKey(row.key) ? 'error' : undefined"
        @update:value="(value: string) => updateRow(index, { key: value })"
      />
      <NInput
        :value="row.value"
        class="mono"
        placeholder="production or secret:db-url"
        aria-label="Variable value"
        @update:value="(value: string) => updateRow(index, { value })"
      />
      <NButton quaternary type="error" aria-label="Remove variable" @click="removeRow(index)">
        <template #icon>
          <NIcon>
            <GothamIcon name="trash" />
          </NIcon>
        </template>
      </NButton>
      <span v-if="isSecretValue(row.value)" class="env-editor__secret">sealed secret</span>
    </div>
    <NButton secondary size="small" @click="addRow">
      Add variable
    </NButton>
    <p class="env-editor__hint">
      Names must match <code class="inline-code">^[A-Z][A-Z0-9_]*$</code>. Values
      starting with <code class="inline-code">secret:</code> reference a sealed
      secret and are never returned by the API.
    </p>
  </div>
</template>

<style scoped>
.env-editor {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  align-items: flex-start;
  /* Container for the single-column fallback below (JUS-19 fix 1): the row
   * grid follows the editor width, not the viewport. */
  container-type: inline-size;
}

.env-editor__empty {
  margin-bottom: var(--space-1);
}

/* One variable is one row: name | value | delete. The grid (not a wrapping
 * flex) keeps the three controls on a single row at modal width; the sealed
 * badge drops onto its own line only when present. */
.env-editor__row {
  display: grid;
  grid-template-columns: minmax(140px, 220px) minmax(0, 1fr) auto;
  gap: var(--space-2);
  align-items: center;
  width: 100%;
  min-width: 0;
}

.env-editor__secret {
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

@container (max-width: 480px) {
  .env-editor__row {
    grid-template-columns: minmax(0, 1fr);
  }

  .env-editor__row > .n-button {
    justify-self: start;
  }
}

.env-editor__hint {
  margin: var(--space-1) 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.inline-code {
  font-family: var(--font-mono);
  font-size: 0.9em;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 1px 6px;
}

.mono {
  font-family: var(--font-mono);
}
</style>
