<script setup lang="ts">
import { NAlert, NSpace, NSpin, NTag } from "naive-ui";

import CodeEditor from "@/features/files/components/CodeEditor.vue";
import type { TemplateRender } from "@/features/templates/api/templates";
import { activeLocale, i18n } from "@/shared/i18n";

interface Props {
  renderLoading: boolean;
  renderError: string | null;
  render: TemplateRender | null;
  secretKeys: string[];
}

defineProps<Props>();

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

function unit(one: string, other: string, count: number): string {
  return t(count === 1 ? one : other, { count });
}
</script>

<template>
  <section class="wizard__step" data-testid="wizard-step-2">
    <NSpin :show="renderLoading">
      <div class="wizard__preview-body">
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
          <div class="wizard__preview" data-testid="compose-preview">
            <CodeEditor
              :model-value="render.compose_yaml"
              preset="yaml"
              read-only
              wrap
              height="40vh"
              :aria-label="t('templates.wizard.steps.preview')"
            />
          </div>
          <NAlert type="info" :show-icon="true">
            {{ t("templates.preview.secretNote", { ref: "${field}" }) }}
            <template v-if="secretKeys.length > 0">
              {{ t("templates.preview.heldSeparately", { keys: secretKeys.join(", ") }) }}
            </template>
          </NAlert>
        </template>
      </div>
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

.wizard__preview-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.wizard__preview {
  min-width: 0;
}
</style>
