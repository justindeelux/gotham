<script setup lang="ts">
import { NAlert, NButton, NText } from "naive-ui";

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

defineProps<Props>();
defineEmits<Emits>();
</script>

<template>
  <div v-if="created === null">
    <NText>Review the database before creating it:</NText>
    <ul class="review-list mono">
      <li>Engine / image · {{ imagePreview }}</li>
      <li>
        Node ·
        {{ serverLabel }}
      </li>
      <li>Name · {{ form.name.trim() }}</li>
      <li>
        Public port ·
        {{
          form.exposePublic
            ? `${form.publicPort} → ${enginePort}`
            : `off · internal network only`
        }}
      </li>
      <li>Volume · kept 7 days after deletion</li>
    </ul>
  </div>
  <div v-else class="success-panel">
    <NAlert type="success" :show-icon="true" title="Database created">
      Container provisioning started on the node. Save these credentials
      — they stay available on the database detail page.
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
        Copy
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
