<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NModal,
  NPopconfirm,
  NSpace,
  NTag,
  NText,
} from "naive-ui";
import { toRef } from "vue";
import { RouterLink } from "vue-router";

import type { Application } from "@/features/applications/api/applications";
import { useCertificateConfig } from "@/features/applications/composables/useCertificateConfig";
import {
  certificateStatusLabel,
  certificateStatusTagType,
} from "@/features/domains";
import CertificateForm from "@/features/domains/components/CertificateForm.vue";
import { expiryLabel, formatDate, relativeTime } from "@/shared/utils/format";

interface Props {
  application: Application;
}

const props = defineProps<Props>();

const certs = useCertificateConfig(toRef(props, "application"));
</script>

<template>
  <NCard title="Certificate configuration">
    <template #header-extra>
      <NButton
        v-if="certs.certificate.value"
        size="small"
        @click="certs.openCertificateEdit"
      >
        Edit
      </NButton>
    </template>
    <NSpace vertical :size="12">
      <NAlert
        v-if="certs.proxyStore.certificatesError"
        type="error"
        :show-icon="true"
      >
        {{ certs.proxyStore.certificatesError }}
      </NAlert>
      <NAlert
        v-if="certs.recordedDomainDiffers.value && certs.certificate.value"
        type="warning"
        :show-icon="true"
      >
        This configuration still records
        <span class="mono">{{ certs.certificate.value.domain }}</span>. Saving it again
        records the current base domain
        <span class="mono">{{ props.application.base_domain }}</span>.
        <NSpace style="margin-top: 8px">
          <NButton size="small" @click="certs.handleRerecordDomain">
            Re-record domain
          </NButton>
        </NSpace>
      </NAlert>

      <template v-if="certs.certificate.value">
        <NDescriptions
          :column="1"
          bordered
          label-placement="left"
          size="small"
        >
          <NDescriptionsItem label="Recorded domain">
            <span class="mono">{{ certs.certificate.value.domain }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem label="Challenge">
            <span class="mono">{{ certs.certificate.value.challenge }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem label="DNS provider">
            <span class="mono">{{ certs.providerName(certs.certificate.value.dns_provider_id) }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem label="Wildcard">
            <NTag v-if="certs.certificate.value.wildcard" size="small">requested</NTag>
            <NText v-else depth="3">no</NText>
          </NDescriptionsItem>
          <NDescriptionsItem label="Enabled">
            <NTag :type="certs.certificate.value.enabled ? 'success' : 'default'" size="small">
              {{ certs.certificate.value.enabled ? "enabled" : "disabled" }}
            </NTag>
          </NDescriptionsItem>
          <NDescriptionsItem label="Status">
            <NTag size="small" :type="certificateStatusTagType(certs.certificate.value.status)">
              {{ certificateStatusLabel(certs.certificate.value.status) }}
            </NTag>
          </NDescriptionsItem>
          <NDescriptionsItem label="Expires">
            <template v-if="certs.certificate.value.status === 'present' && certs.certificate.value.not_after">
              <span class="mono">{{ formatDate(certs.certificate.value.not_after) }}</span>
              <span class="small hint"> · {{ expiryLabel(certs.certificate.value.not_after) }}</span>
            </template>
            <NText v-else-if="certs.certificate.value.status === 'present'" depth="3">
              not reported
            </NText>
            <NText v-else depth="3">—</NText>
          </NDescriptionsItem>
          <NDescriptionsItem label="Updated">
            {{ relativeTime(certs.certificate.value.updated_at) }}
          </NDescriptionsItem>
        </NDescriptions>
        <NSpace :size="8">
          <NPopconfirm
            :positive-button-props="{ type: 'error' }"
            @positive-click="certs.handleDeleteCertificate"
          >
            <template #trigger>
              <NButton size="small" type="error" ghost>
                Delete configuration
              </NButton>
            </template>
            Delete the certificate configuration for
            {{ props.application.base_domain || "this application" }}? The route
            falls back to plain HTTP.
          </NPopconfirm>
        </NSpace>
        <NText depth="3" class="small">
          Status and expiry are observed from the node's ACME storage on
          read. unknown means the node could not be read — never a
          fabricated status.
        </NText>
      </template>

      <NEmpty
        v-else
        description="No certificate configuration for this application yet."
      >
        <template #extra>
          <NButton type="primary" @click="certs.openCertificateEdit">
            Configure certificate
          </NButton>
        </template>
      </NEmpty>
    </NSpace>
    <template #footer>
      <NText depth="3" class="small">
        One configuration per application.
        <RouterLink :to="{ name: 'domains' }">
          Manage all certificates and DNS providers
        </RouterLink>
      </NText>
    </template>
  </NCard>

  <NModal
    v-model:show="certs.certificateOpen.value"
    preset="card"
    :title="certs.certificate.value ? 'Edit certificate configuration' : 'Configure certificate'"
    style="width: 560px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NAlert
        v-if="certs.certificateError.value"
        type="error"
        :show-icon="true"
      >
        {{ certs.certificateError.value }}
      </NAlert>
      <CertificateForm
        v-model="certs.certificateDraft.value"
        :applications="[props.application]"
        :providers="certs.proxyStore.providers"
        lock-application
      />
      <NText depth="3" class="small">
        The domain is not edited here — it comes from the application's base
        domain above.
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="certs.certificateOpen.value = false">Cancel</NButton>
        <NButton
          type="primary"
          :loading="certs.certificateSaving.value"
          @click="certs.handleSaveCertificate"
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

.hint {
  color: var(--muted);
}
</style>
