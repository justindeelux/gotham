<script setup lang="ts">
import {
  NAlert,
  NFormItem,
  NInput,
  NInputNumber,
  NSpace,
  NSwitch,
  NText,
} from "naive-ui";
import { inject } from "vue";

import { wizardFormKey } from "@/features/databases/composables/useCreateDatabaseWizard";
import { NAME_PATTERN } from "@/features/databases/utils/databaseNames";

const form = inject(wizardFormKey)!;
</script>

<template>
  <NFormItem
    label="Name"
    :feedback="
      form.name === '' || NAME_PATTERN.test(form.name.trim())
        ? 'Used for the container and the credentials; 1-63 chars: letters, digits, ., _ or -.'
        : 'Name must be 1-63 characters of letters, digits, ., _ or -.'
    "
    :validation-status="
      form.name === '' || NAME_PATTERN.test(form.name.trim())
        ? undefined
        : 'error'
    "
  >
    <NInput
      v-model:value="form.name"
      class="mono"
      placeholder="pg-orders"
    />
  </NFormItem>
  <NFormItem :show-feedback="false">
    <NSpace align="center" :size="12">
      <NSwitch v-model:value="form.exposePublic" />
      <NText>Expose a public port</NText>
    </NSpace>
  </NFormItem>
  <NAlert v-if="form.exposePublic" type="warning" :show-icon="true">
    A public port is an attack surface and cannot change later — Docker
    port bindings are fixed at creation. Leave it off unless an external
    client requires it.
  </NAlert>
  <NFormItem
    v-if="form.exposePublic"
    label="Public port"
    feedback="Host port forwarding to the engine port."
  >
    <NInputNumber
      v-model:value="form.publicPort"
      :min="1"
      :max="65535"
      placeholder="e.g. 15432"
      style="width: 100%"
    />
  </NFormItem>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
