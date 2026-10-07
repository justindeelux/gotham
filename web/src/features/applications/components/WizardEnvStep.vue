<script setup lang="ts">
import { NAlert, NSpace, NText } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import EnvEditor from "@/features/applications/components/EnvEditor.vue";
import StorageEditor from "@/features/applications/components/StorageEditor.vue";
import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";

const wizard = useCreateWizardState();
const { form } = wizard;

const { t } = useI18n();

/** droppedText renders the 1/many grammar through library pluralization. */
const droppedText = computed<string>(() =>
  String(
    t(
      "applications.wizard.droppedRows",
      { count: wizard.droppedEnvRows.value },
      wizard.droppedEnvRows.value,
    ),
  ),
);
</script>

<template>
  <NSpace vertical :size="12">
    <div>
      <NText strong>{{ t("applications.wizard.envTitle") }}</NText>
      <EnvEditor v-model="form.env" />
    </div>
    <div>
      <NText strong>{{ t("applications.wizard.volumesTitle") }}</NText>
      <StorageEditor v-model="form.storage" />
    </div>
    <NAlert v-if="wizard.envKeyWarnings.value" type="warning" :show-icon="false">
      {{ t("applications.wizard.envConvention") }}
    </NAlert>
    <NAlert v-if="wizard.droppedEnvRows.value > 0" type="warning" :show-icon="false">
      {{ droppedText }}
    </NAlert>
  </NSpace>
</template>
