<script setup lang="ts">
import { NAlert, NSpace, NText } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";

const wizard = useCreateWizardState();
const { form } = wizard;

const { t } = useI18n();

/** reviewDroppedText renders the 1/many grammar through library pluralization. */
const reviewDroppedText = computed<string>(() =>
  String(
    t(
      "applications.wizard.reviewDropped",
      { count: wizard.droppedEnvRows.value },
      wizard.droppedEnvRows.value,
    ),
  ),
);

/** envVolumesText renders the raw counts; numbers are locale-independent. */
const envVolumesText = computed<string>(() =>
  String(
    t("applications.wizard.reviewVolumes", {
      env: form.env.length,
      storage: form.storage.length,
    }),
  ),
);
</script>

<template>
  <NSpace vertical :size="12">
    <NText strong>{{ t("applications.wizard.reviewTitle") }}</NText>
    <dl class="review">
      <dt>{{ t("applications.wizard.reviewApp") }}</dt>
      <dd class="mono">{{ form.name }}</dd>
      <dt>{{ t("applications.wizard.reviewSource") }}</dt>
      <dd class="mono">{{ wizard.reviewSource.value }}</dd>
      <template v-if="!wizard.isComposePaste.value">
        <dt>{{ t("applications.wizard.reviewBuildPack") }}</dt>
        <dd>{{ wizard.buildPackLabel.value }}</dd>
      </template>
      <dt>{{ t("applications.wizard.reviewPortDomain") }}</dt>
      <dd class="mono">{{ form.port ?? 3000 }} · {{ form.baseDomain || "—" }}</dd>
      <dt>{{ t("applications.wizard.reviewEnvVolumes") }}</dt>
      <dd class="mono">{{ envVolumesText }}</dd>
    </dl>
    <NText depth="3">
      {{ t("applications.wizard.reviewIntro") }}
    </NText>
    <NAlert v-if="wizard.droppedEnvRows.value > 0" type="warning" :show-icon="false">
      {{ reviewDroppedText }}
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
