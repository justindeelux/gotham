<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NSwitch,
  NText,
} from "naive-ui";

import { useProviders } from "@/features/domains/composables/useProviders";

const {
  providerOpen,
  providerSaving,
  providerError,
  editingProvider,
  providerForm,
  clearProviderCredential,
  handleSaveProvider,
} = useProviders();

const providerTypeOptions = [
  { label: "Cloudflare", value: "cloudflare" },
  { label: "DigitalOcean", value: "digitalocean" },
];
</script>

<template>
  <NModal
    v-model:show="providerOpen"
    preset="card"
    :title="editingProvider ? 'Edit DNS provider' : 'Add DNS provider'"
    style="width: 560px; max-width: 94vw"
    @after-leave="clearProviderCredential"
  >
    <NSpace vertical :size="12">
      <NAlert v-if="providerError" type="error" :show-icon="true">
        {{ providerError }}
      </NAlert>
      <NForm label-placement="top" :show-feedback="false" class="provider-form form-container">
        <div class="form-row">
          <NFormItem label="Provider" class="field-provider">
            <NSelect
              v-model:value="providerForm.provider"
              :options="providerTypeOptions"
              aria-label="Provider type"
            />
          </NFormItem>
          <NFormItem label="Name" class="field-name">
            <NInput
              v-model:value="providerForm.name"
              placeholder="Optional label, e.g. Production Cloudflare"
              aria-label="Provider name"
            />
          </NFormItem>
        </div>
        <NFormItem label="Zones" class="field-zones">
          <NSelect
            v-model:value="providerForm.zones"
            multiple
            filterable
            tag
            :options="[]"
            placeholder="Type a zone and press Enter"
            aria-label="DNS zones"
          />
        </NFormItem>
        <NFormItem
          :label="editingProvider ? 'Rotate credential (optional)' : 'Credential'"
          class="field-credential"
        >
          <NInput
            v-model:value="providerForm.credential"
            type="password"
            show-password-on="click"
            autocomplete="new-password"
            :placeholder="
              editingProvider
                ? 'Leave blank to keep the stored credential'
                : 'API token'
            "
            aria-label="Provider credential"
          />
        </NFormItem>
        <NFormItem label="Enabled">
          <NSwitch
            v-model:value="providerForm.enabled"
            aria-label="Provider enabled"
          />
        </NFormItem>
      </NForm>
      <NText depth="3" class="small">
        The credential is sent once over the API and sealed server-side; it is
        never displayed, logged or stored in the browser again.
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="providerOpen = false">Cancel</NButton>
        <NButton
          type="primary"
          :loading="providerSaving"
          @click="handleSaveProvider"
        >
          Save
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}
</style>
