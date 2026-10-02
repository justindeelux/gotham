<script setup lang="ts">
import { NButton, NIcon, NInput, NText } from "naive-ui";
import { computed } from "vue";

import type { StorageMapping } from "../api/applications";
import { useStableRowKeys } from "../composables/useStableRowKeys";
import GothamIcon from "./GothamIcon.vue";

/**
 * Editor for the volume map of an application: a named persistent directory on
 * the node (`host_path`) mounted into the container (`container_path`).
 * Every new deploy reuses the same volumes — rollback never loses data.
 */

interface Props {
  modelValue: StorageMapping[];
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:modelValue": [value: StorageMapping[]];
}>();

const rows = computed<StorageMapping[]>(() => props.modelValue);
const { keys: rowKeys, insertAt, removeAt } = useStableRowKeys(() => rows.value.length);

/** updateRow replaces one row, keeping the array immutable for v-model. */
function updateRow(index: number, patch: Partial<StorageMapping>): void {
  const next = rows.value.map((row, rowIndex) =>
    rowIndex === index ? { ...row, ...patch } : row,
  );
  emit("update:modelValue", next);
}

/** addRow appends an empty row for the next volume. */
function addRow(): void {
  insertAt(rows.value.length);
  emit("update:modelValue", [
    ...rows.value,
    { name: "", host_path: "", container_path: "" },
  ]);
}

/** removeRow drops one row by index. */
function removeRow(index: number): void {
  removeAt(index);
  emit("update:modelValue", rows.value.filter((_, rowIndex) => rowIndex !== index));
}
</script>

<template>
  <div class="storage-editor">
    <div v-if="rows.length === 0" class="storage-editor__empty">
      <NText depth="3">No volumes yet. The container filesystem is ephemeral until a volume is added.</NText>
    </div>
    <div
      v-for="(row, index) in rows"
      :key="rowKeys[index] ?? index"
      class="storage-editor__row"
    >
      <NInput
        :value="row.name"
        class="mono"
        placeholder="uploads"
        aria-label="Volume name"
        @update:value="(value: string) => updateRow(index, { name: value })"
      />
      <NInput
        :value="row.host_path"
        class="mono"
        placeholder="Optional — Gotham manages it"
        aria-label="Host path on the node (optional)"
        @update:value="(value: string) => updateRow(index, { host_path: value })"
      />
      <NInput
        :value="row.container_path"
        class="mono"
        placeholder="/app/public/uploads"
        aria-label="Container path"
        @update:value="(value: string) => updateRow(index, { container_path: value })"
      />
      <NButton quaternary type="error" aria-label="Remove volume" @click="removeRow(index)">
        <template #icon>
          <NIcon>
            <GothamIcon name="trash" />
          </NIcon>
        </template>
      </NButton>
    </div>
    <NButton secondary size="small" @click="addRow">
      Add volume
    </NButton>
    <p class="storage-editor__hint">
      Data lives on the node, not in the image. Each column is
      <span class="mono">name → host path → container path</span>. Leave the host
      path blank for a Gotham-managed volume; an explicit path must be inside
      <span class="mono">/var/lib/gotham/volumes/&lt;app id&gt;</span>.
    </p>
  </div>
</template>

<style scoped>
.storage-editor {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  align-items: flex-start;
}

.storage-editor__empty {
  margin-bottom: var(--space-1);
}

.storage-editor__row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1fr) auto;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
}

.storage-editor__hint {
  margin: var(--space-1) 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}

@media (max-width: 720px) {
  .storage-editor__row {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
