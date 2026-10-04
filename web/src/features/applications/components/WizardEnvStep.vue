<script setup lang="ts">
import { NAlert, NSpace, NText } from "naive-ui";

import EnvEditor from "@/features/applications/components/EnvEditor.vue";
import StorageEditor from "@/features/applications/components/StorageEditor.vue";
import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";

const wizard = useCreateWizardState();
const { form } = wizard;
</script>

<template>
  <NSpace vertical :size="12">
    <div>
      <NText strong>Environment variables</NText>
      <EnvEditor v-model="form.env" />
    </div>
    <div>
      <NText strong>Volumes</NText>
      <StorageEditor v-model="form.storage" />
    </div>
    <NAlert v-if="wizard.envKeyWarnings.value" type="warning" :show-icon="false">
      One or more variable names do not follow the usual
      ^[A-Z][A-Z0-9_]*$ convention. The API accepts them, so they are
      not blocked — but a non-standard name may not be injected as you
      expect.
    </NAlert>
    <NAlert v-if="wizard.droppedEnvRows.value > 0" type="warning" :show-icon="false">
      {{ wizard.droppedEnvRows.value }} variable row{{ wizard.droppedEnvRows.value === 1 ? "" : "s" }}
      without a name will be ignored on create.
    </NAlert>
  </NSpace>
</template>
