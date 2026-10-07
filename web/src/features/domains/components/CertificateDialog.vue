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
    :title="editingCertificate ? $t('domains.certificateDialog.editTitle') : $t('domains.certificateDialog.addTitle')"
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
        {{ $t("domains.certificateDialog.note") }}
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="certificateOpen = false">{{ $t("domains.certificateDialog.cancel") }}</NButton>
        <NButton
          type="primary"
          :loading="certificateSaving"
          @click="handleSaveCertificate"
        >
          {{ $t("domains.certificateDialog.save") }}
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
