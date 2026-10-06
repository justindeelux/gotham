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
import { computed, toRef } from "vue";
import { useI18n } from "vue-i18n";
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

const { t } = useI18n();

/** deleteConfirm names the actual domain being removed, never raw HTML. */
const deleteConfirm = computed<string>(() =>
  String(
    t("applications.cert.deleteConfirm", {
      domain: props.application.base_domain || String(t("applications.cert.thisApp")),
    }),
  ),
);
</script>

<template>
  <NCard :title="t('applications.cert.title')">
    <template #header-extra>
      <NButton
        v-if="certs.certificate.value"
        size="small"
        @click="certs.openCertificateEdit"
      >
        {{ t("applications.cert.edit") }}
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
        {{ t("applications.cert.rerecordHintStart") }}
        <span class="mono">{{ certs.certificate.value.domain }}</span>.
        {{ t("applications.cert.rerecordHintMiddle") }}
        <span class="mono">{{ props.application.base_domain }}</span>.
        <NSpace style="margin-top: 8px">
          <NButton size="small" @click="certs.handleRerecordDomain">
            {{ t("applications.cert.rerecord") }}
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
          <NDescriptionsItem :label="t('applications.cert.recordedDomain')">
            <span class="mono">{{ certs.certificate.value.domain }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('applications.cert.challenge')">
            <span class="mono">{{ certs.certificate.value.challenge }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('applications.cert.dnsProvider')">
            <span class="mono">{{ certs.providerName(certs.certificate.value.dns_provider_id) }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('applications.cert.wildcard')">
            <NTag v-if="certs.certificate.value.wildcard" size="small">{{ t("applications.cert.requested") }}</NTag>
            <NText v-else depth="3">{{ t("applications.cert.no") }}</NText>
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('applications.cert.enabled')">
            <NTag :type="certs.certificate.value.enabled ? 'success' : 'default'" size="small">
              {{ certs.certificate.value.enabled ? t("applications.cert.enabledOn") : t("applications.cert.enabledOff") }}
            </NTag>
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('applications.cert.status')">
            <NTag size="small" :type="certificateStatusTagType(certs.certificate.value.status)">
              {{ certificateStatusLabel(certs.certificate.value.status) }}
            </NTag>
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('applications.cert.expires')">
            <template v-if="certs.certificate.value.status === 'present' && certs.certificate.value.not_after">
              <span class="mono">{{ formatDate(certs.certificate.value.not_after) }}</span>
              <span class="small hint"> · {{ expiryLabel(certs.certificate.value.not_after) }}</span>
            </template>
            <NText v-else-if="certs.certificate.value.status === 'present'" depth="3">
              {{ t("applications.cert.notReported") }}
            </NText>
            <NText v-else depth="3">—</NText>
          </NDescriptionsItem>
          <NDescriptionsItem :label="t('applications.cert.updated')">
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
                {{ t("applications.cert.delete") }}
              </NButton>
            </template>
            {{ deleteConfirm }}
          </NPopconfirm>
        </NSpace>
        <NText depth="3" class="small">
          {{ t("applications.cert.observedNote") }}
        </NText>
      </template>

      <NEmpty
        v-else
        :description="t('applications.cert.empty')"
      >
        <template #extra>
          <NButton type="primary" @click="certs.openCertificateEdit">
            {{ t("applications.cert.configure") }}
          </NButton>
        </template>
      </NEmpty>
    </NSpace>
    <template #footer>
      <NText depth="3" class="small">
        {{ t("applications.cert.footerStart") }}
        <RouterLink :to="{ name: 'domains' }">
          {{ t("applications.cert.footerLink") }}
        </RouterLink>
      </NText>
    </template>
  </NCard>

  <NModal
    v-model:show="certs.certificateOpen.value"
    preset="card"
    :title="certs.certificate.value ? t('applications.cert.editTitle') : t('applications.cert.createTitle')"
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
        {{ t("applications.cert.domainNote") }}
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="certs.certificateOpen.value = false">{{ t("common.actions.cancel") }}</NButton>
        <NButton
          type="primary"
          :loading="certs.certificateSaving.value"
          @click="certs.handleSaveCertificate"
        >
          {{ t("common.actions.save") }}
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
