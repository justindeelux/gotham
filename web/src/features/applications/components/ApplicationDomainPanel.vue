<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NInput,
  NInputNumber,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import { computed, onMounted, toRef } from "vue";
import { useI18n } from "vue-i18n";

import type { Application } from "@/features/applications/api/applications";
import { useApplicationDomains } from "@/features/applications/composables/useApplicationDomains";
import {
  certificateStatusLabel,
  certificateStatusTagType,
  useProxyStore,
  type Certificate,
} from "@/features/domains";

interface Props {
  application: Application;
}

const props = defineProps<Props>();

const domains = useApplicationDomains(toRef(props, "application"));
const proxyStore = useProxyStore();

const { t } = useI18n();

const protocolOptions = computed(() => [
  { label: t("applications.domain.protocolHttp"), value: "http" },
  { label: t("applications.domain.protocolHttps"), value: "https" },
]);

const formTitle = computed(() =>
  domains.editingId.value === null
    ? t("applications.domain.addTitle")
    : t("applications.domain.editTitle"),
);

/** certOf resolves the certificate intent recorded for one hostname. */
function certOf(host: string): Certificate | null {
  return (
    proxyStore.certificates.find(
      (item) => item.application_id === props.application.id && item.domain === host,
    ) ?? null
  );
}

onMounted(() => {
  void proxyStore.fetchCertificates().catch(() => undefined);
});
</script>

<template>
  <NSpace vertical :size="16">
    <NCard :title="t('applications.domain.title')">
      <NSpace vertical :size="12">
        <NText depth="3">
          {{ t("applications.domain.hint") }}
        </NText>
        <NAlert v-if="domains.domainsError.value" type="error" :show-icon="true">
          {{ domains.domainsError.value }}
        </NAlert>

        <NCard size="small" :title="formTitle">
          <NSpace vertical :size="10">
            <NAlert v-if="domains.formError.value" type="error" :show-icon="true">
              {{ domains.formError.value }}
            </NAlert>
            <div class="domain-form">
              <NInput
                v-model:value="domains.draft.domain"
                class="mono domain-host"
                placeholder="app.example.com"
                :input-props="{ 'aria-label': t('applications.domain.hostAria') }"
              />
              <NInputNumber
                v-model:value="domains.draft.port"
                class="domain-port"
                :min="1"
                :max="65535"
                placeholder="3000"
                :input-props="{ 'aria-label': t('applications.domain.portAria') }"
              />
              <NSelect
                v-if="domains.isCompose.value"
                v-model:value="domains.draft.service"
                class="domain-service"
                filterable
                tag
                clearable
                :options="domains.serviceOptions.value"
                :placeholder="t('applications.domain.servicePlaceholder')"
                :aria-label="t('applications.domain.serviceAria')"
                @update:value="domains.handleServiceChange"
              />
              <NSelect
                v-model:value="domains.draft.protocol"
                class="domain-protocol"
                :options="protocolOptions"
                :aria-label="t('applications.domain.protocolAria')"
              />
            </div>
            <NSpace :size="8">
              <NButton
                type="primary"
                :loading="domains.saving.value"
                @click="domains.handleSave"
              >
                {{ domains.editingId.value === null ? t("applications.domain.add") : t("applications.domain.save") }}
              </NButton>
              <NButton v-if="domains.editingId.value !== null" ghost @click="domains.resetDraft">
                {{ t("applications.domain.cancel") }}
              </NButton>
            </NSpace>
            <NText v-if="domains.isCompose.value" depth="3" class="small">
              {{ t("applications.domain.serviceHint") }}
            </NText>
          </NSpace>
        </NCard>

        <NSpin :show="domains.loadingDomains.value">
          <NSpace v-if="domains.domains.value.length > 0" vertical :size="8">
            <div v-for="row in domains.domains.value" :key="row.id" class="domain-row">
              <span class="mono domain-name">{{ row.domain }}</span>
              <NTag size="small">
                {{ row.port }}
              </NTag>
              <NTag v-if="row.service" size="small" type="info">
                {{ row.service }}
              </NTag>
              <NTag size="small" :type="row.protocol === 'https' ? 'success' : 'default'">
                {{ row.protocol }}
              </NTag>
              <NTag v-if="row.is_primary" size="small" type="success">
                {{ t("applications.domain.primary") }}
              </NTag>
              <NTag v-if="row.disabled" size="small" type="warning">
                {{ t("applications.domain.disabled") }}
              </NTag>
              <NTag
                v-if="certOf(row.domain)"
                size="small"
                :type="certificateStatusTagType(certOf(row.domain)?.status)"
              >
                {{ certificateStatusLabel(certOf(row.domain)?.status) }}
              </NTag>
              <NButton
                v-else
                size="small"
                ghost
                :disabled="row.disabled"
                :loading="domains.busyDomainId.value === row.id"
                @click="domains.handleSecureDomain(row)"
              >
                {{ t("applications.domain.secure") }}
              </NButton>
              <span class="domain-actions">
                <NButton
                  size="small"
                  :loading="domains.busyDomainId.value === row.id"
                  @click="domains.startEdit(row)"
                >
                  {{ t("applications.domain.edit") }}
                </NButton>
                <NButton
                  v-if="!row.is_primary"
                  size="small"
                  :disabled="row.disabled"
                  :loading="domains.busyDomainId.value === row.id"
                  @click="domains.handleSetPrimary(row)"
                >
                  {{ t("applications.domain.setPrimary") }}
                </NButton>
                <NPopconfirm @positive-click="domains.handleRemoveDomain(row)">
                  <template #trigger>
                    <NButton
                      size="small"
                      type="error"
                      ghost
                      :loading="domains.busyDomainId.value === row.id"
                    >
                      {{ t("applications.domain.remove") }}
                    </NButton>
                  </template>
                  {{ t("applications.domain.removeConfirm", { domain: row.domain }) }}
                </NPopconfirm>
              </span>
              <NAlert
                v-if="domains.rowErrorId.value === row.id && domains.rowError.value"
                type="error"
                :show-icon="true"
                class="row-error"
              >
                {{ domains.rowError.value }}
              </NAlert>
            </div>
          </NSpace>
          <NText v-else depth="3" class="small">
            {{ t("applications.domain.empty") }}
          </NText>
        </NSpin>
        <NText depth="3" class="small">
          {{ t("applications.domain.note") }}
        </NText>
      </NSpace>
    </NCard>
  </NSpace>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}

.domain-form {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 12px;
}

.domain-host {
  min-width: 220px;
  max-width: 360px;
}

.domain-port {
  width: 110px;
}

.domain-service,
.domain-protocol {
  width: 160px;
}

.domain-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 12px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}

.domain-name {
  min-width: 0;
  overflow-wrap: anywhere;
}

.domain-actions {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-left: auto;
}

.row-error {
  flex-basis: 100%;
}
</style>
