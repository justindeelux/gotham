<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NFormItem,
  NInputNumber,
  NSelect,
  NSpace,
  NText,
} from "naive-ui";
import { computed, ref, toRef } from "vue";
import { useI18n } from "vue-i18n";

import type {
  Application,
  ApplicationDomain,
  DomainProtocol,
} from "@/features/applications/api/applications";
import CertificatePanel from "@/features/applications/components/CertificatePanel.vue";
import DomainTable from "@/features/applications/components/DomainTable.vue";
import WebsiteChecksCard from "@/features/applications/components/WebsiteChecksCard.vue";
import { useApplicationDomains } from "@/features/applications/composables/useApplicationDomains";
import { useProxyStore, type Certificate } from "@/features/domains";
import DomainFormModal from "@/features/domains/components/DomainFormModal.vue";

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

const showRenameWarning = computed<boolean>(
  () => renameCertificateHost.value !== "",
);

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
          :diagnostic-of="domains.routeDiagnostic"
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

    <WebsiteChecksCard
      :application="props.application"
      :domains="domains.domains.value"
    />

    <DomainFormModal
      v-model:show="domains.formOpen.value"
      :title="
        domains.editingId.value !== null
          ? t('domains.form.editTitle')
          : t('domains.form.addTitle')
      "
      :submit-label="
        domains.editingId.value !== null
          ? t('domains.form.save')
          : t('domains.form.add')
      "
      :host="domains.draft.domain"
      :protocol="domains.draft.protocol"
      :protocol-options="protocolOptions"
      :saving="domains.saving.value"
      :error="domains.formError.value"
      :hint="t('applications.domain.portHint')"
      @update:host="domains.draft.domain = $event"
      @update:protocol="domains.draft.protocol = $event"
      @save="domains.handleSave"
      @cancel="domains.closeForm"
    >
      <template #warning>
        <NAlert v-if="showRenameWarning" type="warning" :show-icon="true">
          {{ t("applications.domain.renameCertWarning", { domain: renameCertificateHost }) }}
        </NAlert>
      </template>
      <template #routing>
        <NFormItem :label="t('applications.domain.port')" class="domain-routing-field">
          <NInputNumber
            :value="domains.draft.port"
            class="domain-port"
            :min="1"
            :max="65535"
            :placeholder="t('applications.domain.portPlaceholder')"
            :input-props="{ 'aria-label': t('applications.domain.portAria') }"
            @update:value="(value: number | null) => (domains.draft.port = value)"
          />
        </NFormItem>
      </template>
      <template v-if="domains.isCompose.value" #extra>
        <NFormItem :label="t('applications.domain.service')">
          <NSelect
            :value="domains.draft.service"
            class="domain-service"
            filterable
            tag
            clearable
            :options="domains.serviceOptions.value"
            :placeholder="t('applications.domain.servicePlaceholder')"
            :aria-label="t('applications.domain.serviceAria')"
            @update:value="(value: string | null) => domains.handleServiceChange(value)"
          />
        </NFormItem>
      </template>
      <template v-if="domains.isCompose.value" #hint>
        <NText depth="3" class="small">
          {{ t("applications.domain.serviceHint") }}
        </NText>
      </template>
    </DomainFormModal>

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

.domain-port {
  width: 100%;
}

.domain-service {
  width: 100%;
}

.domain-routing-field {
  flex: 1;
  min-width: 0;
}
</style>
