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
</style>
