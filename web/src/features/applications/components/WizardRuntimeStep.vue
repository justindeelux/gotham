<script setup lang="ts">
import { NFormItem, NInput, NInputNumber, NSpace } from "naive-ui";
import { useI18n } from "vue-i18n";

import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";
import ServerPicker from "@/features/projects/components/ServerPicker.vue";

const wizard = useCreateWizardState();
const { form } = wizard;

const { t } = useI18n();
</script>

<template>
  <NSpace vertical :size="16">
    <div class="form-row">
      <ServerPicker v-model="form.serverId" :label="t('applications.wizard.node')" />

      <NFormItem :label="t('applications.wizard.domainOptional')">
        <NInput
          v-model:value="form.baseDomain"
          class="mono"
          placeholder="app.gotham.dev"
        />
        <span class="field-hint">{{ t("applications.wizard.domainHint") }}</span>
      </NFormItem>
    </div>

    <div class="form-row">
      <NFormItem :label="t('applications.wizard.internalPort')">
        <NInputNumber
          v-model:value="form.port"
          :min="1"
          :max="65535"
          placeholder="3000"
        />
        <span class="field-hint">{{ t("applications.wizard.internalPortHint") }}</span>
      </NFormItem>
      <NFormItem :label="t('applications.wizard.hostPort')">
        <NInputNumber
          v-model:value="form.hostPort"
          :min="0"
          :max="65535"
          placeholder="0"
        />
        <span class="field-hint">{{ t("applications.wizard.hostPortHint") }}</span>
      </NFormItem>
    </div>
  </NSpace>
</template>

<style scoped>
.field-hint {
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}
</style>
