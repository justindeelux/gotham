<script setup lang="ts">
import { NAlert, NButton, NInput, NSpace } from "naive-ui";
import { useI18n } from "vue-i18n";

import type { BuildArgRow } from "@/features/applications/utils/buildArgs";

/**
 * Key/value editor for --build-arg pairs, shared by the wizard Source step
 * and the detail Dockerfile editor. Values render masked with click-to-reveal
 * and carry a no-secrets warning: ARG values are stored in plaintext,
 * visible to every app reader, and persist in image history on the node.
 */

interface Props {
  modelValue: BuildArgRow[];
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:modelValue": [rows: BuildArgRow[]];
}>();

const { t } = useI18n();

/** setRow replaces one row, keeping the draft immutable for the parent. */
function setRow(index: number, patch: Partial<BuildArgRow>): void {
  emit(
    "update:modelValue",
    props.modelValue.map((row, rowIndex) =>
      rowIndex === index ? { ...row, ...patch } : row,
    ),
  );
}

/** addRow appends an empty draft row. */
function addRow(): void {
  emit("update:modelValue", [...props.modelValue, { key: "", value: "" }]);
}

/** removeRow drops one row by index. */
function removeRow(index: number): void {
  emit(
    "update:modelValue",
    props.modelValue.filter((_, rowIndex) => rowIndex !== index),
  );
}
</script>

<template>
  <NSpace vertical :size="8">
    <NAlert type="warning" :show-icon="true">
      {{ t("applications.buildArgs.secretWarning") }}
    </NAlert>
    <div v-for="(row, index) in props.modelValue" :key="index" class="arg-row">
      <NInput
        :value="row.key"
        class="mono"
        :placeholder="t('applications.wizard.buildArgKey')"
        :input-props="{ 'aria-label': t('applications.wizard.buildArgKey') }"
        @update:value="setRow(index, { key: String($event) })"
      />
      <NInput
        :value="row.value"
        type="password"
        show-password-on="click"
        class="mono"
        :placeholder="t('applications.wizard.buildArgValue')"
        :input-props="{ 'aria-label': t('applications.wizard.buildArgValue') }"
        @update:value="setRow(index, { value: String($event) })"
      />
      <NButton quaternary size="small" :aria-label="t('applications.buildArgs.remove')" @click="removeRow(index)">
        {{ t("applications.buildArgs.remove") }}
      </NButton>
    </div>
    <NButton dashed size="small" @click="addRow">
      {{ t("applications.buildArgs.add") }}
    </NButton>
  </NSpace>
</template>

<style scoped>
.arg-row {
  display: grid;
  grid-template-columns: 1fr 1fr auto;
  gap: var(--space-2);
  align-items: center;
}
</style>
