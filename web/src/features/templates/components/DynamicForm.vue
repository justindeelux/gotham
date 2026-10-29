<script setup lang="ts">
import {
  NButton,
  NForm,
  NFormItem,
  NInput,
  NInputGroup,
  NInputNumber,
  NSelect,
  NSwitch,
  useMessage,
} from "naive-ui";
import { computed, nextTick, ref } from "vue";

import type { TemplateField, TemplateValues } from "@/features/templates/api/templates";
import { templateOverlayFieldHelp } from "@/features/templates/api/templates";
import { activeLocale, i18n } from "@/shared/i18n";
import { generateSecret } from "@/shared/utils/generateSecret";

interface SecretInput {
  passwordVisible: boolean;
}

interface Props {
  fields: TemplateField[];
  modelValue: TemplateValues;
  errors?: Record<string, string>;
  disabled?: boolean;
  slug?: string;
}

const props = withDefaults(defineProps<Props>(), {
  errors: () => ({}),
  disabled: false,
  slug: "",
});

const emit = defineEmits<{
  "update:modelValue": [value: TemplateValues];
}>();

const message = useMessage();
const secretInputs = new Map<string, SecretInput>();
const announcement = ref("");

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

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

function fieldValue(field: TemplateField): string {
  return props.modelValue[field.key] ?? "";
}

function numberValue(field: TemplateField): number | null {
  const raw = fieldValue(field).trim();
  if (raw === "") {
    return null;
  }
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : null;
}

function helpText(field: TemplateField): string | undefined {
  if (props.slug !== "") {
    return templateOverlayFieldHelp(props.slug, field);
  }
  return field.help;
}

function feedbackText(field: TemplateField): string {
  return props.errors[field.key] ?? helpText(field) ?? "";
}

function validationStatus(field: TemplateField): "error" | undefined {
  return props.errors[field.key] ? "error" : undefined;
}

function setValue(key: string, value: string): void {
  emit("update:modelValue", { ...props.modelValue, [key]: value });
}

function setSecretInput(key: string, input: unknown): void {
  if (input && typeof input === "object" && "passwordVisible" in input) {
    secretInputs.set(key, input as SecretInput);
  } else {
    secretInputs.delete(key);
  }
}

function fillGenerated(field: TemplateField): void {
  const length = field.max_length ? Math.min(24, field.max_length) : 24;
  setValue(field.key, generateSecret(length));
  announcement.value = t("templates.secret.generated", { label: field.label });
  void nextTick(() => {
    const input = secretInputs.get(field.key);
    if (input) {
      input.passwordVisible = true;
    }
  });
}

async function copyValue(field: TemplateField): Promise<void> {
  try {
    await navigator.clipboard.writeText(fieldValue(field));
    announcement.value = t("templates.secret.copiedAnnouncement");
    message.success(t("templates.secret.copied", { label: field.label }));
  } catch {
    message.warning(t("templates.secret.copyFailed", { label: field.label }));
  }
}
</script>

<template>
  <NForm label-placement="top" class="dynamic-form form--grid">
    <p class="sr-only" role="status" aria-live="polite">{{ announcement }}</p>
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

      <template v-else-if="field.type === 'secret'">
        <NInputGroup v-if="field.generate" class="dynamic-form__secret-group">
          <NInput
            :ref="(input) => setSecretInput(field.key, input)"
            type="password"
            show-password-on="click"
            :input-props="{ autocomplete: 'new-password' }"
            :value="fieldValue(field)"
            :placeholder="field.placeholder ?? ''"
            :disabled="disabled"
            :aria-label="field.label"
            @update:value="(value: string) => setValue(field.key, value)"
          />
          <div class="dynamic-form__secret-actions">
            <NButton
              size="small"
              secondary
              :disabled="disabled"
              :aria-label="t('templates.secret.generateLabel', { label: field.label })"
              @click="fillGenerated(field)"
            >
              {{ t("templates.secret.generate") }}
            </NButton>
            <NButton
              v-if="fieldValue(field) !== ''"
              size="small"
              secondary
              :disabled="disabled"
              :aria-label="t('templates.secret.copyLabel', { label: field.label })"
              @click="copyValue(field)"
            >
              {{ t("templates.secret.copy") }}
            </NButton>
          </div>
        </NInputGroup>

        <NInput
          v-else
          type="password"
          show-password-on="click"
          :input-props="{ autocomplete: 'new-password' }"
          :value="fieldValue(field)"
          :placeholder="field.placeholder ?? ''"
          :disabled="disabled"
          :aria-label="field.label"
          @update:value="(value: string) => setValue(field.key, value)"
        />
      </template>

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
  gap: var(--form-item-gap) var(--space-4);
  align-items: start;
}

.dynamic-form__item {
  min-width: 0;
}

.dynamic-form__secret-group {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: var(--space-2);
}

.dynamic-form__secret-group :deep(.n-input) {
  min-width: 0;
  border-radius: var(--radius-sm) !important;
}

.dynamic-form__secret-actions {
  display: flex;
  gap: var(--space-2);
  justify-content: flex-start;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

@media (max-width: 720px) {
  .dynamic-form {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
