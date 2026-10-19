<script setup lang="ts">
import { NText, NSpace } from "naive-ui";
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
    </div>
    <NText depth="3">{{ t("applications.wizard.routingHint") }}</NText>
  </NSpace>
</template>

<style scoped>
.form-row {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
}
</style>
