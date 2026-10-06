<script setup lang="ts">
import { NAlert, NButton, NText } from "naive-ui";
import { computed } from "vue";

import type { CreatedDatabase } from "@/features/databases/api/databases";
import type { WizardForm } from "@/features/databases/composables/useCreateDatabaseWizard";

interface Props {
  form: WizardForm;
  imagePreview: string;
  serverLabel: string;
  enginePort: number;
  created: CreatedDatabase | null;
  credentialRows: Array<{ label: string; value: string; secret: boolean }>;
}

interface Emits {
  copy: [value: string, label: string];
}

import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const props = defineProps<Props>();
defineEmits<Emits>();


/** portMapping renders the raw ports; only the surrounding words localize. */
const portMapping = computed<string>(() =>
  props.form.exposePublic
    ? `${props.form.publicPort} → ${props.enginePort}`
    : String(t("databases.wizard.review.publicPortOff")),
);
</script>

<template>
  <div v-if="created === null">
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
  <div v-else class="success-panel">
    <NAlert
      type="success"
      :show-icon="true"
      :title="t('databases.wizard.review.successTitle')"
    >
      {{ t("databases.wizard.review.successBody") }}
    </NAlert>
    <div
      v-for="row in credentialRows"
      :key="row.label"
      class="credential-row"
    >
      <NText depth="3">{{ row.label }}</NText>
      <NText class="mono">{{ row.value }}</NText>
      <NButton
        size="small"
        secondary
        @click="$emit('copy', row.value, row.label)"
      >
        {{ t("databases.detail.credentials.copy") }}
      </NButton>
    </div>
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

.success-panel {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.credential-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.credential-row .mono {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
