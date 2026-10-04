<script setup lang="ts">
import { NAlert, NSpace, NSpin, NTag } from "naive-ui";

import type { TemplateRender } from "@/features/templates/api/templates";

interface Props {
  renderLoading: boolean;
  renderError: string | null;
  render: TemplateRender | null;
  secretKeys: string[];
}

defineProps<Props>();
</script>

<template>
  <section class="wizard__step" data-testid="wizard-step-2">
    <NSpin :show="renderLoading">
      <NAlert v-if="renderError" type="error" :show-icon="true">
        {{ renderError }}
      </NAlert>
      <template v-else-if="render">
        <NSpace :size="8" align="center">
          <NTag size="small">{{ render.spec.services.length }} services</NTag>
          <NTag size="small">
            {{ render.spec.named_volumes.length }} named volumes
          </NTag>
          <NTag size="small">
            {{ render.spec.domains.length }} domain routes
          </NTag>
        </NSpace>
        <pre class="wizard__preview mono" data-testid="compose-preview">{{
          render.compose_yaml
        }}</pre>
        <NAlert type="info" :show-icon="true">
          The engine only substitutes strings. A secret field stays a
          <span class="mono">${field}</span> reference in this document;
          its value travels separately in the service environment and is
          redacted from errors and deploy history.
          <template v-if="secretKeys.length > 0">
            Held separately: <span class="mono">{{ secretKeys.join(", ") }}</span
            >.
          </template>
        </NAlert>
      </template>
    </NSpin>
  </section>
</template>

<style scoped>
.wizard__step {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.wizard__preview {
  margin: 0;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-3);
  font-size: var(--text-xs);
  line-height: 1.6;
  color: var(--fg);
  overflow: auto;
  max-height: 40vh;
  white-space: pre-wrap;
  word-break: break-word;
}

.mono {
  font-family: var(--font-mono);
}
</style>
