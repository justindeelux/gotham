<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NPopconfirm,
  NSpace,
  NTag,
  NText,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { h } from "vue";
import type { VNode } from "vue";
import { computed } from "vue";

import {
  certificateStatusLabel,
  certificateStatusTagType,
  proxyText,
} from "@/features/domains/api/proxy";
import type { Certificate } from "@/features/domains/api/proxy";
import { useCertificates } from "@/features/domains/composables/useCertificates";
import { useDomainLabels } from "@/features/domains/composables/useDomainLabels";
import { useProxyStore } from "@/features/domains/stores/proxy";
import { expiryLabel, formatDate } from "@/shared/utils/format";

const proxyStore = useProxyStore();
const { openCertificateCreate, openCertificateEdit, handleDeleteCertificate } =
  useCertificates();
const { applicationName } = useDomainLabels();

const cellStackStyle = {
  display: "flex",
  flexDirection: "column",
  gap: "var(--space-1)",
  minWidth: "0",
} as const;

const cellNameStyle = {
  color: "var(--fg-2)",
  fontWeight: 600,
} as const;

const cellSubStyle = {
  fontSize: "var(--text-xs)",
  color: "var(--muted)",
} as const;

function domainCell(certificate: Certificate): VNode {
  return h("div", { style: cellStackStyle }, [
    h("span", { class: "mono", style: cellNameStyle }, certificate.domain),
    h(
      "span",
      { style: cellSubStyle },
      applicationName(certificate.application_id),
    ),
  ]);
}

function statusCell(certificate: Certificate): VNode {
  return h(
    NTag,
    {
      size: "small",
      type: certificateStatusTagType(certificate.status),
    },
    { default: () => certificateStatusLabel(certificate.status) },
  );
}

function expiryCell(certificate: Certificate): VNode {
  if (certificate.status !== "present") {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  if (!certificate.not_after) {
    return h(
      NText,
      { depth: 3 },
      {
        default: () =>
          proxyText(
            "domains.certificates.expiryNotReported",
            "not reported",
          ),
      },
    );
  }
  return h("div", { style: cellStackStyle }, [
    h("span", { class: "mono" }, formatDate(certificate.not_after)),
    h("span", { style: cellSubStyle }, expiryLabel(certificate.not_after)),
  ]);
}

function certificateActions(certificate: Certificate): VNode {
  return h(NSpace, { size: 8, align: "center", wrap: false }, {
    default: () => [
      h(
        NButton,
        { size: "small", onClick: () => openCertificateEdit(certificate) },
        {
          default: () => proxyText("domains.certificates.edit", "Edit"),
        },
      ),
      h(
        NPopconfirm,
        {
          positiveButtonProps: { type: "error" },
          onPositiveClick: () => void handleDeleteCertificate(certificate),
        },
        {
          trigger: () =>
            h(
              NButton,
              { size: "small", type: "error", ghost: true },
              {
                default: () =>
                  proxyText("domains.certificates.delete", "Delete"),
              },
            ),
          default: () =>
            proxyText(
              "domains.certificates.deleteConfirm",
              "Delete the certificate configuration for {domain}? The route " +
                "falls back to plain HTTP.",
              { domain: certificate.domain },
            ),
        },
      ),
    ],
  });
}

const certificateColumns = computed<DataTableColumns<Certificate>>(() => [
  {
    title: proxyText("domains.certificates.domain", "Domain"),
    key: "domain",
    minWidth: 240,
    render: (row) => domainCell(row),
  },
  {
    title: proxyText("domains.certificates.status", "Status"),
    key: "status",
    width: 150,
    render: (row) => statusCell(row),
  },
  {
    title: proxyText("domains.certificates.expires", "Expires"),
    key: "not_after",
    minWidth: 180,
    render: (row) => expiryCell(row),
  },
  {
    title: proxyText("domains.certificates.actions", "Actions"),
    key: "actions",
    width: 160,
    render: (row) => certificateActions(row),
  },
]);

function certificateRowKey(row: Certificate): string {
  return row.id;
}
</script>

<template>
  <NCard style="margin-top: 16px" :title="$t('domains.certificates.title')">
    <template #header-extra>
      <NText depth="3">{{ $t("domains.certificates.onePerApp") }}</NText>
    </template>
    <NSpace vertical :size="12">
      <NAlert
        v-if="proxyStore.certificatesError"
        type="error"
        :show-icon="true"
      >
        {{ proxyStore.certificatesError }}
      </NAlert>
      <NDataTable
        v-if="proxyStore.certificates.length > 0 || proxyStore.certificatesLoading"
        :columns="certificateColumns"
        :data="proxyStore.certificates"
        :loading="proxyStore.certificatesLoading"
        :row-key="certificateRowKey"
        :bordered="false"
        :scroll-x="720"
        :pagination="{ pageSize: 10 }"
      />
      <NEmpty v-else :description="$t('domains.certificates.empty')">
        <template #extra>
          <NButton type="primary" @click="openCertificateCreate">
            {{ $t("domains.certificates.add") }}
          </NButton>
        </template>
      </NEmpty>
      <div class="embed">
        <h4>{{ $t("domains.certificates.statusTitle") }}</h4>
        <p>
          {{ $t("domains.certificates.statusBody") }}
        </p>
      </div>
    </NSpace>
    <template #footer>
      <NText depth="3">
        {{ $t("domains.certificates.footer") }}
      </NText>
    </template>
  </NCard>
</template>

<style scoped>
.embed {
  border-left: 4px solid var(--accent);
  background: var(--surface);
  border-radius: var(--radius-sm);
  padding: var(--space-2) var(--space-3);
}

.embed h4 {
  font-size: var(--text-sm);
}

.embed p {
  font-size: var(--text-xs);
  color: var(--muted);
  margin-top: var(--space-1);
}
</style>
