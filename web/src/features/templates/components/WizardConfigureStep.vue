<script setup lang="ts">
import type {
  TemplateField,
  TemplateValues,
} from "@/features/templates/api/templates";
import DynamicForm from "./DynamicForm.vue";

interface Props {
  slug: string;
  fields: TemplateField[];
  values: TemplateValues;
  errors: Record<string, string>;
}

defineProps<Props>();

const emit = defineEmits<{
  "update:values": [values: TemplateValues];
}>();
</script>

<template>
  <section class="wizard__step" data-testid="wizard-step-1">
    <p class="wizard__desc">
      Fields come from the template schema
      (<span class="mono">GET /api/v1/templates/{{ slug }}</span>);
      adding a template does not require a UI change. Secret values stay
      in this form until they are sent as the service environment.
    </p>
    <DynamicForm
      :model-value="values"
      :fields="fields"
      :errors="errors"
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
