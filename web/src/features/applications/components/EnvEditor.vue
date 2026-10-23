<script setup lang="ts">
import { NButton, NIcon, NInput, NText } from "naive-ui";
import { computed } from "vue";
import { useI18n, I18nT } from "vue-i18n";

import type { EnvVar } from "@/features/applications/api/applications";
import { isRecommendedEnvKey } from "@/features/applications/schemas/env";
import { useStableRowKeys } from "@/shared/composables/useStableRowKeys";
import { isSecretValue } from "@/features/applications/utils/envSecret";
import GothamIcon from "@/shared/ui/GothamIcon.vue";

interface Props {
  modelValue: EnvVar[];
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:modelValue": [value: EnvVar[]];
}>();

const { t } = useI18n();

const rows = computed<EnvVar[]>(() => props.modelValue);
const { keys: rowKeys, insertAt, removeAt } = useStableRowKeys(() => rows.value.length);

function isValidKey(key: string): boolean {
  return isRecommendedEnvKey(key);
}

function updateRow(index: number, patch: Partial<EnvVar>): void {
  const next = rows.value.map((row, rowIndex) =>
    rowIndex === index ? { ...row, ...patch } : row,
  );
  emit("update:modelValue", next);
}

function addRow(): void {
  insertAt(rows.value.length);
  emit("update:modelValue", [...rows.value, { key: "", value: "" }]);
}

function removeRow(index: number): void {
  removeAt(index);
  emit("update:modelValue", rows.value.filter((_, rowIndex) => rowIndex !== index));
}
</script>

<template>
  <div class="env-editor">
    <div v-if="rows.length === 0" class="env-editor__empty">
      <NText depth="3">{{ t("applications.envEditor.empty") }}</NText>
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
        :input-props="{ 'aria-label': t('applications.envEditor.nameAria'), autocomplete: 'off' }"
        :status="row.key !== '' && !isValidKey(row.key) ? 'error' : undefined"
        @update:value="(value: string) => updateRow(index, { key: value })"
      />
      <NInput
        :value="row.value"
        class="mono"
        placeholder="production or secret:db-url"
        :input-props="{ 'aria-label': t('applications.envEditor.valueAria'), autocomplete: 'off' }"
        @update:value="(value: string) => updateRow(index, { value })"
      />
      <NButton quaternary type="error" :aria-label="t('applications.envEditor.removeAria')" @click="removeRow(index)">
        <template #icon>
          <NIcon>
            <GothamIcon name="trash" />
          </NIcon>
        </template>
      </NButton>
      <span v-if="isSecretValue(row.value)" class="env-editor__secret">{{ t("applications.envEditor.sealed") }}</span>
    </div>
    <NButton secondary size="small" @click="addRow">
      {{ t("applications.envEditor.add") }}
    </NButton>
    <i18n-t keypath="applications.envEditor.hint" tag="p" class="env-editor__hint">
      <template #pattern><code class="inline-code">^[A-Z][A-Z0-9_]*$</code></template>
      <template #prefix><code class="inline-code">secret:</code></template>
    </i18n-t>
  </div>
</template>

<style scoped>
.env-editor {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  align-items: flex-start;
  container-type: inline-size;
}

.env-editor__empty {
  margin-bottom: var(--space-1);
}

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
