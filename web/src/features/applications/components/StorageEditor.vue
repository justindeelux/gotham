<script setup lang="ts">
import { NButton, NIcon, NInput, NText } from "naive-ui";
import { computed } from "vue";
import { useI18n, I18nT } from "vue-i18n";

import type { StorageMapping } from "@/features/applications/api/applications";
import { useStableRowKeys } from "@/shared/composables/useStableRowKeys";
import GothamIcon from "@/shared/ui/GothamIcon.vue";

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

const { t } = useI18n();

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
      <NText depth="3">{{ t("applications.storageEditor.empty") }}</NText>
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
        :input-props="{ 'aria-label': t('applications.storageEditor.nameAria') }"
        @update:value="(value: string) => updateRow(index, { name: value })"
      />
      <NInput
        :value="row.host_path"
        class="mono"
        :placeholder="t('applications.storageEditor.hostPlaceholder')"
        :input-props="{ 'aria-label': t('applications.storageEditor.hostAria') }"
        @update:value="(value: string) => updateRow(index, { host_path: value })"
      />
      <NInput
        :value="row.container_path"
        class="mono"
        placeholder="/app/public/uploads"
        :input-props="{ 'aria-label': t('applications.storageEditor.containerAria') }"
        @update:value="(value: string) => updateRow(index, { container_path: value })"
      />
      <NButton quaternary type="error" :aria-label="t('applications.storageEditor.removeAria')" @click="removeRow(index)">
        <template #icon>
          <NIcon>
            <GothamIcon name="trash" />
          </NIcon>
        </template>
      </NButton>
    </div>
    <NButton secondary size="small" @click="addRow">
      {{ t("applications.storageEditor.add") }}
    </NButton>
    <i18n-t keypath="applications.storageEditor.hint" tag="p" class="storage-editor__hint">
      <template #columns><span class="mono">{{ t("applications.storageEditor.columns") }}</span></template>
      <template #path><span class="mono">/var/lib/gotham/volumes/&lt;app id&gt;</span></template>
    </i18n-t>
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
