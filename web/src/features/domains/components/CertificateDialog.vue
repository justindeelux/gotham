<script setup lang="ts">
import { NAlert, NButton, NModal, NSpace, NText } from "naive-ui";

import CertificateForm from "@/features/domains/components/CertificateForm.vue";
import { useCertificates } from "@/features/domains/composables/useCertificates";
import { useProxyStore } from "@/features/domains/stores/proxy";

const proxyStore = useProxyStore();
const {
  certificateOpen,
  certificateSaving,
  certificateError,
  editingCertificate,
  certificateDraft,
  handleSaveCertificate,
} = useCertificates();
</script>

<template>
  <NModal
    v-model:show="certificateOpen"
    preset="card"
    :title="editingCertificate ? 'Edit certificate configuration' : 'Add certificate'"
    style="width: 560px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NAlert
        v-if="certificateError"
        type="error"
        :show-icon="true"
      >
        {{ certificateError }}
      </NAlert>
      <CertificateForm
        v-model="certificateDraft"
        :applications="proxyStore.applications"
        :providers="proxyStore.providers"
        :lock-application="editingCertificate !== null"
      />
      <NText depth="3" class="small">
        The recorded domain always comes from the selected application's base
        domain — it is not editable here.
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="certificateOpen = false">Cancel</NButton>
        <NButton
          type="primary"
          :loading="certificateSaving"
          @click="handleSaveCertificate"
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
