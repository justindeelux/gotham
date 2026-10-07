<script setup lang="ts">
import type {
  TemplateField,
  TemplateValues,
} from "@/features/templates/api/templates";
import { activeLocale, i18n } from "@/shared/i18n";
import DynamicForm from "./DynamicForm.vue";

interface Props {
  slug: string;
  fields: TemplateField[];
  values: TemplateValues;
  errors: Record<string, string>;
}

defineProps<Props>();

/**
 * t renders step copy in the active locale (tracks language switches).
 * The endpoint stays a raw parameter, never a translated key.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const emit = defineEmits<{
  "update:values": [values: TemplateValues];
}>();
</script>

<template>
  <section class="wizard__step" data-testid="wizard-step-1">
    <p class="wizard__desc">
      {{ t("templates.configure.description", { endpoint: `GET /api/v1/templates/${slug}` }) }}
    </p>
    <DynamicForm
      :model-value="values"
      :fields="fields"
      :errors="errors"
      :slug="slug"
      @update:model-value="(next) => emit('update:values', next)"
    />
  </section>
</template>

<style scoped>
.wizard__step {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.wizard__desc {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}
</style>
