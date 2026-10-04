<script setup lang="ts">
import { NFormItem, NInput, NInputNumber, NSelect, NSpace } from "naive-ui";

import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";

const wizard = useCreateWizardState();
const { form } = wizard;
</script>

<template>
  <NSpace vertical :size="16">
    <div class="form-row">
      <NFormItem label="Node">
        <NSelect
          v-model:value="form.serverId"
          :options="wizard.serverOptions.value"
          placeholder="Select the node that runs the container"
        />
        <span class="field-hint">The agent builds the image on this node.</span>
      </NFormItem>

      <NFormItem label="Domain (optional)">
        <NInput
          v-model:value="form.baseDomain"
          class="mono"
          placeholder="app.gotham.dev"
        />
        <span class="field-hint">Leave empty to reach the app by port first.</span>
      </NFormItem>
    </div>

    <div class="form-row">
      <NFormItem label="Internal port">
        <NInputNumber
          v-model:value="form.port"
          :min="1"
          :max="65535"
          placeholder="3000"
        />
        <span class="field-hint">The port the app listens on inside the container.</span>
      </NFormItem>
      <NFormItem label="Host port (0 = auto)">
        <NInputNumber
          v-model:value="form.hostPort"
          :min="0"
          :max="65535"
          placeholder="0"
        />
        <span class="field-hint">Leave empty to let the control plane assign one.</span>
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
