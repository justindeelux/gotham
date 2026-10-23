<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NSpace, NText } from "naive-ui";
import { computed, ref, toRef } from "vue";
import { useI18n } from "vue-i18n";

import type {
  Application,
  ApplicationDomain,
  DomainProtocol,
} from "@/features/applications/api/applications";
import CertificatePanel from "@/features/applications/components/CertificatePanel.vue";
import DomainFormModal from "@/features/applications/components/DomainFormModal.vue";
import DomainTable from "@/features/applications/components/DomainTable.vue";
import { useApplicationDomains } from "@/features/applications/composables/useApplicationDomains";
import { useProxyStore, type Certificate } from "@/features/domains";

interface Props {
  application: Application;
}

const props = defineProps<Props>();

const domains = useApplicationDomains(toRef(props, "application"));
const proxyStore = useProxyStore();

const { t } = useI18n();

const protocolOptions = computed<Array<{ label: string; value: DomainProtocol }>>(() => [
  { label: t("applications.domain.protocolHttp"), value: "http" },
  { label: t("applications.domain.protocolHttps"), value: "https" },
]);

function certificateOf(host: string): Certificate | null {
  return (
    proxyStore.certificates.find(
      (item) => item.application_id === props.application.id && item.domain === host,
    ) ?? null
  );
}

const editingRow = computed<ApplicationDomain | null>(() => {
  if (domains.editingId.value === null) {
    return null;
  }
  return domains.domains.value.find((row) => row.id === domains.editingId.value) ?? null;
});

const renameCertificateHost = computed<string>(() => {
  const row = editingRow.value;
  if (row === null) {
    return "";
  }
  const next = domains.draft.domain.trim().toLowerCase();
  if (next === "" || next === row.domain) {
    return "";
  }
  return certificateOf(row.domain)?.domain ?? "";
});

const certificateOpen = ref(false);
const certificateDomain = ref("");

function openCertificate(row: ApplicationDomain): void {
  certificateDomain.value = row.domain;
  certificateOpen.value = true;
}
</script>

<template>
  <NSpace vertical :size="16">
    <NCard :title="t('applications.domain.title')">
      <template #header-extra>
        <NButton type="primary" size="small" class="domain-add" @click="domains.openCreate">
          {{ t("applications.domain.add") }}
        </NButton>
      </template>
      <NSpace vertical :size="12">
        <NText depth="3">
          {{ t("applications.domain.hint") }}
        </NText>
        <NAlert v-if="domains.domainsError.value" type="error" :show-icon="true">
          {{ domains.domainsError.value }}
        </NAlert>
        <NAlert v-if="domains.rowError.value" type="error" :show-icon="true">
          {{ domains.rowError.value }}
        </NAlert>

        <DomainTable
          v-if="domains.domains.value.length > 0 || domains.loadingDomains.value"
          :rows="domains.domains.value"
          :loading="domains.loadingDomains.value"
          :busy-domain-id="domains.busyDomainId.value"
          :certificate-of="certificateOf"
          @edit="domains.startEdit"
          @set-primary="domains.handleSetPrimary"
          @certificate="openCertificate"
          @remove="domains.handleRemoveDomain"
        />
        <NEmpty v-else :description="t('applications.domain.empty')" />

        <NText depth="3" class="small">
          {{ t("applications.domain.note") }}
        </NText>
      </NSpace>
    </NCard>

    <DomainFormModal
      v-model:show="domains.formOpen.value"
      :editing="domains.editingId.value !== null"
      :domain="domains.draft.domain"
      :port="domains.draft.port"
      :service="domains.draft.service"
      :protocol="domains.draft.protocol"
      :saving="domains.saving.value"
      :error="domains.formError.value"
      :is-compose="domains.isCompose.value"
      :service-options="domains.serviceOptions.value"
      :protocol-options="protocolOptions"
      :rename-certificate-host="renameCertificateHost"
      @update:domain="domains.draft.domain = $event"
      @update:port="domains.draft.port = $event"
      @update:protocol="domains.draft.protocol = $event"
      @service-change="domains.handleServiceChange"
      @save="domains.handleSave"
      @cancel="domains.closeForm"
    />

    <CertificatePanel
      v-model:show="certificateOpen"
      :application="props.application"
      :domain="certificateDomain"
    />
  </NSpace>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}
</style>
