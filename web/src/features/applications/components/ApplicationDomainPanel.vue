<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NTag,
  NText,
  type DataTableColumns,
} from "naive-ui";
import { computed, h, onMounted, ref, toRef } from "vue";
import { useI18n } from "vue-i18n";

import type {
  Application,
  ApplicationDomain,
} from "@/features/applications/api/applications";
import CertificatePanel from "@/features/applications/components/CertificatePanel.vue";
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

const submitLabel = computed(() =>
  domains.editingId.value === null
    ? t("applications.domain.add")
    : t("applications.domain.save"),
);

/** certificateOpen and certificateDomain drive the per-row certificate modal. */
const certificateOpen = ref(false);
const certificateDomain = ref("");

/** certOf resolves the certificate intent recorded for one hostname. */
function certOf(host: string): Certificate | null {
  return (
    proxyStore.certificates.find(
      (item) => item.application_id === props.application.id && item.domain === host,
    ) ?? null
  );
}

/** openCertificate targets exactly the clicked row's domain. */
function openCertificate(row: ApplicationDomain): void {
  certificateDomain.value = row.domain;
  certificateOpen.value = true;
}

/** rowProps tags every data row for tests and the layout proof. */
function rowProps(): Record<string, unknown> {
  return { class: "domain-row" };
}

const columns = computed<DataTableColumns<ApplicationDomain>>(() => [
  {
    title: t("applications.domain.host"),
    key: "domain",
    minWidth: 220,
    render: (row) => h("span", { class: "mono domain-name" }, row.domain),
  },
  {
    title: t("applications.domain.port"),
    key: "port",
    width: 90,
    render: (row) => h("span", { class: "mono" }, String(row.port)),
  },
  {
    title: t("applications.domain.service"),
    key: "service",
    minWidth: 120,
    render: (row) =>
      row.service
        ? h(NTag, { size: "small", type: "info" }, { default: () => row.service })
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: t("applications.domain.protocol"),
    key: "protocol",
    width: 110,
    render: (row) =>
      h(
        NTag,
        { size: "small", type: row.protocol === "https" ? "success" : "default" },
        { default: () => row.protocol },
      ),
  },
  {
    title: t("applications.domain.primaryColumn"),
    key: "is_primary",
    width: 110,
    render: (row) =>
      row.is_primary
        ? h(NTag, { size: "small", type: "success" }, {
            default: () => t("applications.domain.primary"),
          })
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: t("applications.domain.certificate"),
    key: "certificate",
    minWidth: 140,
    render: (row) => {
      const cert = certOf(row.domain);
      if (!cert) {
        return h(NText, { depth: 3 }, {
          default: () => t("applications.domain.noCert"),
        });
      }
      return h(
        NTag,
        { size: "small", type: certificateStatusTagType(cert.status) },
        { default: () => certificateStatusLabel(cert.status) },
      );
    },
  },
  {
    title: t("applications.domain.actions"),
    key: "actions",
    width: 330,
    render: (row) =>
      h(
        NSpace,
        { size: 8, wrap: false, align: "center", class: "domain-actions" },
        {
          default: () => [
            h(
              NButton,
              {
                size: "small",
                loading: domains.busyDomainId.value === row.id,
                onClick: () => domains.startEdit(row),
              },
              { default: () => t("applications.domain.edit") },
            ),
            row.is_primary
              ? null
              : h(
                  NButton,
                  {
                    size: "small",
                    disabled: row.disabled,
                    loading: domains.busyDomainId.value === row.id,
                    onClick: () => void domains.handleSetPrimary(row),
                  },
                  { default: () => t("applications.domain.setPrimary") },
                ),
            h(
              NButton,
              {
                size: "small",
                class: "domain-certificate",
                onClick: () => openCertificate(row),
              },
              { default: () => t("applications.domain.certificate") },
            ),
            h(
              NPopconfirm,
              {
                onPositiveClick: () => void domains.handleRemoveDomain(row),
              },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      size: "small",
                      type: "error",
                      ghost: true,
                      loading: domains.busyDomainId.value === row.id,
                    },
                    { default: () => t("applications.domain.remove") },
                  ),
                default: () =>
                  t("applications.domain.removeConfirm", { domain: row.domain }),
              },
            ),
          ],
        },
      ),
  },
]);

onMounted(() => {
  void proxyStore.fetchCertificates().catch(() => undefined);
});
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

        <NDataTable
          v-if="domains.domains.value.length > 0 || domains.loadingDomains.value"
          class="domain-table"
          :columns="columns"
          :data="domains.domains.value"
          :loading="domains.loadingDomains.value"
          :row-key="(row: ApplicationDomain) => row.id"
          :row-props="rowProps"
          :bordered="false"
          :single-line="false"
          :scroll-x="1080"
          :pagination="false"
        />
        <NEmpty v-else :description="t('applications.domain.empty')" />

        <NText depth="3" class="small">
          {{ t("applications.domain.note") }}
        </NText>
      </NSpace>
    </NCard>

    <NModal
      v-model:show="domains.formOpen.value"
      preset="card"
      :title="formTitle"
      class="app-modal domain-form-modal"
      style="width: 520px; max-width: 94vw"
      :mask-closable="false"
    >
      <NSpace vertical :size="12">
        <NAlert v-if="domains.formError.value" type="error" :show-icon="true">
          {{ domains.formError.value }}
        </NAlert>
        <NForm label-placement="top" :show-feedback="false">
          <NFormItem :label="t('applications.domain.host')">
            <NInput
              v-model:value="domains.draft.domain"
              class="mono domain-host"
              placeholder="app.example.com"
              :input-props="{ 'aria-label': t('applications.domain.hostAria') }"
            />
          </NFormItem>
          <NFormItem :label="t('applications.domain.port')">
            <NInputNumber
              v-model:value="domains.draft.port"
              class="domain-port"
              :min="1"
              :max="65535"
              placeholder="3000"
              :input-props="{ 'aria-label': t('applications.domain.portAria') }"
            />
          </NFormItem>
          <NFormItem
            v-if="domains.isCompose.value"
            :label="t('applications.domain.service')"
          >
            <NSelect
              :value="domains.draft.service"
              class="domain-service"
              filterable
              tag
              clearable
              :options="domains.serviceOptions.value"
              :placeholder="t('applications.domain.servicePlaceholder')"
              :aria-label="t('applications.domain.serviceAria')"
              @update:value="domains.handleServiceChange"
            />
          </NFormItem>
          <NFormItem :label="t('applications.domain.protocol')">
            <NSelect
              v-model:value="domains.draft.protocol"
              class="domain-protocol"
              :options="protocolOptions"
              :aria-label="t('applications.domain.protocolAria')"
            />
          </NFormItem>
        </NForm>
        <NText v-if="domains.isCompose.value" depth="3" class="small">
          {{ t("applications.domain.serviceHint") }}
        </NText>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="domains.closeForm">
            {{ t("applications.domain.cancel") }}
          </NButton>
          <NButton
            type="primary"
            class="domain-submit"
            :loading="domains.saving.value"
            @click="domains.handleSave"
          >
            {{ submitLabel }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

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

.domain-table {
  width: 100%;
}

.domain-table :deep(.domain-row) {
  vertical-align: middle;
}

.domain-table :deep(.domain-name) {
  overflow-wrap: anywhere;
}

.domain-table :deep(.n-data-table-th) {
  white-space: nowrap;
}

.domain-port {
  width: 100%;
  max-width: 180px;
}

.domain-service,
.domain-protocol {
  width: 100%;
  max-width: 320px;
}
</style>
