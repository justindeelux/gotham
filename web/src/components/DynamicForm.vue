<script setup lang="ts">
import {
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSelect,
  NSwitch,
} from "naive-ui";
import { computed } from "vue";

import type { TemplateField, TemplateValues } from "../api/templates";

/**
 * Renders a form from a template's field schema: text/secret input, number,
 * select and bool, with required, pattern, min/max, max_length and help text
 * taken straight from the API (see Field in internal/templates/schema.go).
 *
 * Values are strings in each field's canonical form; validation messages are
 * supplied by the caller (`errors`, computed with checkTemplateValue) so the
 * submit flow decides when they become visible.
 */

interface Props {
  fields: TemplateField[];
  modelValue: TemplateValues;
  errors?: Record<string, string>;
  disabled?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  errors: () => ({}),
  disabled: false,
});

const emit = defineEmits<{
  "update:modelValue": [value: TemplateValues];
}>();

const selectOptions = computed<Record<string, Array<{ label: string; value: string }>>>(
  () => {
    const map: Record<string, Array<{ label: string; value: string }>> = {};
    for (const field of props.fields) {
      map[field.key] = (field.options ?? []).map((option) => ({
        label: option,
        value: option,
      }));
    }
    return map;
  },
);

/** fieldValue returns the current value in canonical string form. */
function fieldValue(field: TemplateField): string {
  return props.modelValue[field.key] ?? "";
}

/** numberValue renders the string value for NInputNumber; empty is null. */
function numberValue(field: TemplateField): number | null {
  const raw = fieldValue(field).trim();
  if (raw === "") {
    return null;
  }
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : null;
}

/** feedbackText shows the validation message, falling back to the help. */
function feedbackText(field: TemplateField): string {
  return props.errors[field.key] ?? field.help ?? "";
}

/** validationStatus marks a field in error without re-validating here. */
function validationStatus(field: TemplateField): "error" | undefined {
  return props.errors[field.key] ? "error" : undefined;
}

/** setValue emits a copy with one field replaced. */
function setValue(key: string, value: string): void {
  emit("update:modelValue", { ...props.modelValue, [key]: value });
}
</script>

<template>
  <NForm label-placement="top" class="dynamic-form">
    <NFormItem
      v-for="field in fields"
      :key="field.key"
      :label="field.label"
      :required="field.required"
      :feedback="feedbackText(field)"
      :validation-status="validationStatus(field)"
      class="dynamic-form__item"
      :class="`field-${field.key}`"
    >
      <NInput
        v-if="field.type === 'text'"
        :value="fieldValue(field)"
        :placeholder="field.placeholder ?? ''"
        :disabled="disabled"
        :aria-label="field.label"
        clearable
        @update:value="(value: string) => setValue(field.key, value)"
      />

      <NInput
        v-else-if="field.type === 'secret'"
        type="password"
        show-password-on="click"
        :input-props="{ autocomplete: 'new-password' }"
        :value="fieldValue(field)"
        :placeholder="field.placeholder ?? ''"
        :disabled="disabled"
        :aria-label="field.label"
        @update:value="(value: string) => setValue(field.key, value)"
      />

      <NInputNumber
        v-else-if="field.type === 'number'"
        :value="numberValue(field)"
        :min="field.min"
        :max="field.max"
        :show-button="false"
        :placeholder="field.placeholder ?? ''"
        :disabled="disabled"
        :aria-label="field.label"
        style="width: 100%"
        @update:value="
          (value: number | null) => setValue(field.key, value === null ? '' : String(value))
        "
      />

      <NSelect
        v-else-if="field.type === 'select'"
        :value="fieldValue(field)"
        :options="selectOptions[field.key] ?? []"
        :placeholder="field.placeholder ?? ''"
        :disabled="disabled"
        :aria-label="field.label"
        @update:value="(value: string) => setValue(field.key, value)"
      />

      <NSwitch
        v-else
        :value="fieldValue(field) === 'true'"
        :disabled="disabled"
        :aria-label="field.label"
        @update:value="(value: boolean) => setValue(field.key, String(value))"
      />
    </NFormItem>
  </NForm>
</template>

<style scoped>
.dynamic-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-4) var(--space-4);
}

.dynamic-form__item {
  min-width: 0;
}

@media (max-width: 720px) {
  .dynamic-form {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
