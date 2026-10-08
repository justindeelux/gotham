<script setup lang="ts">
import { NAlert, NButton, NCheckbox, NSpace } from "naive-ui";
import { useI18n } from "vue-i18n";

import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";
import { useCopyText } from "@/shared/composables/useCopyText";

const wizard = useCreateWizardState();
const { t } = useI18n();
const { copyText } = useCopyText();
</script>

<template>
  <NSpace v-if="wizard.createdKey.value" vertical :size="16">
    <NAlert type="success" :show-icon="true">
      {{ t("applications.privateGit.createdKey", { name: wizard.createdKey.value.application.name }) }}
    </NAlert>

    <div>
      <div class="section-title">{{ t("applications.privateGit.publicKey") }}</div>
      <pre class="mono key">{{ wizard.createdKey.value.publicKey }}</pre>
      <span class="field-hint">{{ t("applications.privateGit.keyHint") }}</span>
      <div style="margin-top: 8px">
        <NButton
          size="small"
          @click="void copyText(wizard.createdKey.value.publicKey, t('applications.privateGit.publicKey'))"
        >
          {{ t("applications.privateGit.copyKey") }}
        </NButton>
      </div>
    </div>

    <div>
      <NButton size="small" :loading="wizard.keyTesting.value" @click="void wizard.runCreatedKeyTest()">
        {{ t("applications.privateGit.test") }}
      </NButton>
      <NAlert
        v-if="wizard.keyTestMessage.value"
        :type="wizard.keyTestPassed.value ? 'success' : 'error'"
        :show-icon="true"
        style="margin-top: 8px"
      >
        {{ wizard.keyTestMessage.value }}
      </NAlert>
    </div>

    <NCheckbox v-model:checked="wizard.keyConfirmed.value">
      {{ t("applications.privateGit.confirmKey") }}
    </NCheckbox>
  </NSpace>
</template>

<style scoped>
.section-title {
  font-weight: 600;
  margin-bottom: 8px;
}

.field-hint {
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}

.key {
  white-space: pre-wrap;
  word-break: break-all;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 8px 12px;
  font-size: var(--text-xs);
}
</style>
