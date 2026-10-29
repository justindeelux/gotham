<script setup lang="ts">
import { NText } from "naive-ui";
import { computed } from "vue";

import type { WizardForm } from "@/features/databases/composables/useCreateDatabaseWizard";

interface Props {
  form: WizardForm;
  imagePreview: string;
  serverLabel: string;
  enginePort: number;
}

import { i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const props = defineProps<Props>();

const portMapping = computed<string>(() =>
  props.form.exposePublic
    ? `${props.form.publicPort} → ${props.enginePort}`
    : String(t("databases.wizard.review.publicPortOff")),
);
</script>

<template>
  <div>
    <NText>{{ t("databases.wizard.review.intro") }}</NText>
    <ul class="review-list mono">
      <li>{{ t("databases.wizard.review.engineImage", { image: imagePreview }) }}</li>
      <li>
        {{ t("databases.wizard.review.node", { server: serverLabel }) }}
      </li>
      <li>{{ t("databases.wizard.review.name", { name: form.name.trim() }) }}</li>
      <li>
        {{ t("databases.wizard.review.publicPort", { mapping: portMapping }) }}
      </li>
      <li>{{ t("databases.wizard.review.volume") }}</li>
    </ul>
  </div>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.review-list {
  list-style: none;
  margin: var(--space-3) 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  color: var(--muted);
}

</style>
