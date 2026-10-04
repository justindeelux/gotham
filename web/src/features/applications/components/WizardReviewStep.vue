<script setup lang="ts">
import { NAlert, NSpace, NText } from "naive-ui";

import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";

const wizard = useCreateWizardState();
const { form } = wizard;
</script>

<template>
  <NSpace vertical :size="12">
    <NText strong>Summary</NText>
    <dl class="review">
      <dt>Application</dt>
      <dd class="mono">{{ form.name }}</dd>
      <dt>Source</dt>
      <dd class="mono">{{ wizard.reviewSource.value }}</dd>
      <dt>Build pack</dt>
      <dd>{{ wizard.buildPackLabel.value }}</dd>
      <dt>Port / domain</dt>
      <dd class="mono">{{ form.port ?? 3000 }} · {{ form.baseDomain || "—" }}</dd>
      <dt>Env / volumes</dt>
      <dd class="mono">{{ form.env.length }} vars · {{ form.storage.length }} volumes</dd>
    </dl>
    <NText depth="3">
      Creating posts the payload to the applications API, then the
      first deploy queues immediately.
    </NText>
    <NAlert v-if="wizard.droppedEnvRows.value > 0" type="warning" :show-icon="false">
      {{ wizard.droppedEnvRows.value }} nameless variable row{{ wizard.droppedEnvRows.value === 1 ? "" : "s" }}
      will be ignored on create.
    </NAlert>
  </NSpace>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.review {
  display: grid;
  grid-template-columns: 140px minmax(0, 1fr);
  gap: var(--space-2) var(--space-3);
  margin: 0;
}

.review dt {
  color: var(--muted);
  font-size: var(--text-sm);
}

.review dd {
  margin: 0;
  color: var(--fg);
  font-size: var(--text-sm);
  word-break: break-word;
}
</style>
