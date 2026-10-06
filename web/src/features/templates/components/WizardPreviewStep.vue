<script setup lang="ts">
import { NAlert, NSpace, NSpin, NTag } from "naive-ui";

import type { TemplateRender } from "@/features/templates/api/templates";
import { activeLocale, i18n } from "@/shared/i18n";

interface Props {
  renderLoading: boolean;
  renderError: string | null;
  render: TemplateRender | null;
  secretKeys: string[];
}

defineProps<Props>();

/**
 * t renders preview copy in the active locale (tracks language switches).
 * The rendered compose document and secret values stay raw and untranslated.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

/** unit picks the singular/plural unit label for a count. */
function unit(one: string, other: string, count: number): string {
  return t(count === 1 ? one : other, { count });
}
</script>

<template>
  <section class="wizard__step" data-testid="wizard-step-2">
    <NSpin :show="renderLoading">
      <NAlert v-if="renderError" type="error" :show-icon="true">
        {{ renderError }}
      </NAlert>
      <template v-else-if="render">
        <NSpace :size="8" align="center">
          <NTag size="small">{{ unit("templates.preview.servicesOne", "templates.preview.servicesOther", render.spec.services.length) }}</NTag>
          <NTag size="small">
            {{
              unit(
                "templates.preview.volumesOne",
                "templates.preview.volumesOther",
                render.spec.named_volumes.length,
              )
            }}
          </NTag>
          <NTag size="small">
            {{
              unit(
                "templates.preview.domainsOne",
                "templates.preview.domainsOther",
                render.spec.domains.length,
              )
            }}
          </NTag>
        </NSpace>
        <pre class="wizard__preview mono" data-testid="compose-preview">{{
          render.compose_yaml
        }}</pre>
        <NAlert type="info" :show-icon="true">
          {{ t("templates.preview.secretNote", { ref: "${field}" }) }}
          <template v-if="secretKeys.length > 0">
            {{ t("templates.preview.heldSeparately", { keys: secretKeys.join(", ") }) }}
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
