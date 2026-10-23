<script setup lang="ts">
import {
  NAlert,
  NButton,
  NDescriptions,
  NDescriptionsItem,
  NModal,
  NPopconfirm,
  NSpace,
  NTag,
  NText,
} from "naive-ui";
import { computed, toRef, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { Application } from "@/features/applications/api/applications";
import { useCertificateConfig } from "@/features/applications/composables/useCertificateConfig";
import {
  certificateStatusLabel,
  certificateStatusTagType,
} from "@/features/domains";
import CertificateForm from "@/features/domains/components/CertificateForm.vue";
import { expiryLabel, formatDate, relativeTime } from "@/shared/utils/format";

interface Props {
  show: boolean;
  application: Application;
  domain: string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:show": [value: boolean];
}>();

const certs = useCertificateConfig(
  toRef(props, "application"),
  toRef(props, "domain"),
);

const { t } = useI18n();

const deleteConfirm = computed<string>(() =>
  String(
    t("applications.cert.deleteConfirm", {
      domain: props.domain || String(t("applications.cert.thisApp")),
    }),
  ),
);

watch(
  () => props.show,
  (show) => {
    if (show) {
      certs.openCertificateEdit();
    }
  },
);

function close(): void {
  emit("update:show", false);
}

async function save(): Promise<void> {
  await certs.handleSaveCertificate();
  if (certs.certificateError.value === null) {
    emit("update:show", false);
  }
}

async function remove(): Promise<void> {
  await certs.handleDeleteCertificate();
  if (certs.certificate.value === null) {
    emit("update:show", false);
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="t('applications.cert.titleFor', { domain: props.domain })"
    class="app-modal certificate-modal"
    style="width: 560px; max-width: 94vw"
    :mask-closable="false"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <NSpace vertical :size="12">
      <NAlert
        v-if="certs.proxyStore.certificatesError"
        type="error"
        :show-icon="true"
      >
        {{ certs.proxyStore.certificatesError }}
      </NAlert>
      <NAlert
        v-if="certs.certificateError.value"
        type="error"
        :show-icon="true"
      >
        {{ certs.certificateError.value }}
      </NAlert>

      <NDescriptions
        v-if="certs.certificate.value"
        :column="1"
        bordered
        label-placement="left"
        size="small"
      >
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

      <CertificateForm
        v-model="certs.certificateDraft.value"
        :applications="[props.application]"
        :providers="certs.proxyStore.providers"
        :domain="props.domain"
        lock-application
      />
      <NText depth="3" class="small">
        {{ t("applications.cert.domainNote") }}
      </NText>
      <NText v-if="certs.certificate.value" depth="3" class="small">
        {{ t("applications.cert.observedNote") }}
      </NText>
    </NSpace>
    <template #footer>
      <NSpace justify="space-between" align="center" :size="8">
        <div>
          <NPopconfirm
            v-if="certs.certificate.value"
            :positive-button-props="{ type: 'error' }"
            @positive-click="remove"
          >
            <template #trigger>
              <NButton size="small" type="error" ghost>
                {{ t("applications.cert.delete") }}
              </NButton>
            </template>
            {{ deleteConfirm }}
          </NPopconfirm>
        </div>
        <NSpace :size="8">
          <NButton @click="close">{{ t("common.actions.cancel") }}</NButton>
          <NButton
            type="primary"
            class="certificate-save"
            :loading="certs.certificateSaving.value"
            @click="save"
          >
            {{ t("common.actions.save") }}
          </NButton>
        </NSpace>
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
