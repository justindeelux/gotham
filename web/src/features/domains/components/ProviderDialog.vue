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
    :title="editingProvider ? $t('domains.providerDialog.editTitle') : $t('domains.providerDialog.addTitle')"
    style="width: 560px; max-width: 94vw"
    @after-leave="clearProviderCredential"
  >
    <NSpace vertical :size="12">
      <NAlert v-if="providerError" type="error" :show-icon="true">
        {{ providerError }}
      </NAlert>
      <NForm label-placement="top" :show-feedback="false" class="provider-form form-container">
        <div class="form-row">
          <NFormItem :label="$t('domains.providerDialog.provider')" class="field-provider">
            <NSelect
              v-model:value="providerForm.provider"
              :options="providerTypeOptions"
              :aria-label="$t('domains.providerDialog.providerAria')"
            />
          </NFormItem>
          <NFormItem :label="$t('domains.providerDialog.name')" class="field-name">
            <NInput
              v-model:value="providerForm.name"
              :placeholder="$t('domains.providerDialog.namePlaceholder')"
              :aria-label="$t('domains.providerDialog.nameAria')"
            />
          </NFormItem>
        </div>
        <NFormItem :label="$t('domains.providerDialog.zones')" class="field-zones">
          <NSelect
            v-model:value="providerForm.zones"
            multiple
            filterable
            tag
            :options="[]"
            :placeholder="$t('domains.providerDialog.zonesPlaceholder')"
            :aria-label="$t('domains.providerDialog.zonesAria')"
          />
        </NFormItem>
        <NFormItem
          :label="editingProvider ? $t('domains.providerDialog.rotateCredential') : $t('domains.providerDialog.credential')"
          class="field-credential"
        >
          <NInput
            v-model:value="providerForm.credential"
            type="password"
            show-password-on="click"
            autocomplete="new-password"
            :placeholder="
              editingProvider
                ? $t('domains.providerDialog.keepCredentialPlaceholder')
                : $t('domains.providerDialog.credentialPlaceholder')
            "
            :aria-label="$t('domains.providerDialog.credentialAria')"
          />
        </NFormItem>
        <NFormItem :label="$t('domains.providerDialog.enabled')">
          <NSwitch
            v-model:value="providerForm.enabled"
            :aria-label="$t('domains.providerDialog.enabledAria')"
          />
        </NFormItem>
      </NForm>
      <NText depth="3" class="small">
        {{ $t("domains.providerDialog.sealedNote") }}
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="providerOpen = false">{{ $t("domains.providerDialog.cancel") }}</NButton>
        <NButton
          type="primary"
          :loading="providerSaving"
          @click="handleSaveProvider"
        >
          {{ $t("domains.providerDialog.save") }}
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
