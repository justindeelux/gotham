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

import {
  certificateStatusLabel,
  certificateStatusTagType,
} from "@/features/domains/api/proxy";
import type { Certificate } from "@/features/domains/api/proxy";
import { useCertificates } from "@/features/domains/composables/useCertificates";
import { useDomainLabels } from "@/features/domains/composables/useDomainLabels";
import { useProxyStore } from "@/features/domains/stores/proxy";
import { expiryLabel, formatDate, relativeTime } from "@/shared/utils/format";

const proxyStore = useProxyStore();
const { openCertificateCreate, openCertificateEdit, handleDeleteCertificate } =
  useCertificates();
const { providerName, applicationName, applicationDomain } = useDomainLabels();

/** domainCell renders the recorded domain with its application. */
function domainCell(certificate: Certificate): VNode {
  const domain = applicationDomain(certificate);
  return h("div", { class: "cell-main" }, [
    h("span", { class: "mono cell-name" }, certificate.domain),
    h(
      "span",
      { class: "cell-sub" },
      domain && domain !== certificate.domain
        ? `${applicationName(certificate.application_id)} · base domain ` +
          `changed to ${domain} — re-save to re-record`
        : applicationName(certificate.application_id),
    ),
  ]);
}

/** statusCell renders the node-observed certificate status. */
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

/** expiryCell renders the live expiry, only ever for a present certificate. */
function expiryCell(certificate: Certificate): VNode {
  if (certificate.status !== "present") {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  if (!certificate.not_after) {
    return h(NText, { depth: 3 }, { default: () => "not reported" });
  }
  return h("div", { class: "cell-main" }, [
    h("span", { class: "mono" }, formatDate(certificate.not_after)),
    h("span", { class: "cell-sub" }, expiryLabel(certificate.not_after)),
  ]);
}

/** actionsCell renders Edit / Delete controls for one certificate. */
function certificateActions(certificate: Certificate): VNode {
  return h(NSpace, { size: 8, align: "center", wrap: false }, {
    default: () => [
      h(
        NButton,
        { size: "small", onClick: () => openCertificateEdit(certificate) },
        { default: () => "Edit" },
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
              { default: () => "Delete" },
            ),
          default: () =>
            `Delete the certificate configuration for ${certificate.domain}? ` +
            "The route falls back to plain HTTP.",
        },
      ),
    ],
  });
}

const certificateColumns: DataTableColumns<Certificate> = [
  {
    title: "Domain",
    key: "domain",
    minWidth: 240,
    render: (row) => domainCell(row),
  },
  {
    title: "Challenge",
    key: "challenge",
    width: 110,
    render: (row) => h("span", { class: "mono" }, row.challenge),
  },
  {
    title: "DNS provider",
    key: "dns_provider_id",
    minWidth: 160,
    render: (row) => h("span", { class: "mono" }, providerName(row.dns_provider_id)),
  },
  {
    title: "Wildcard",
    key: "wildcard",
    width: 110,
    render: (row) =>
      row.wildcard
        ? h(NTag, { size: "small" }, { default: () => "wildcard" })
        : h(NText, { depth: 3 }, { default: () => "no" }),
  },
  {
    title: "Enabled",
    key: "enabled",
    width: 110,
    render: (row) =>
      h(
        NTag,
        { size: "small", type: row.enabled ? "success" : "default" },
        { default: () => (row.enabled ? "enabled" : "disabled") },
      ),
  },
  {
    title: "Status",
    key: "status",
    width: 150,
    render: (row) => statusCell(row),
  },
  {
    title: "Expires",
    key: "not_after",
    minWidth: 180,
    render: (row) => expiryCell(row),
  },
  {
    title: "Updated",
    key: "updated_at",
    width: 120,
    render: (row) => relativeTime(row.updated_at),
  },
  {
    title: "Actions",
    key: "actions",
    width: 160,
    render: (row) => certificateActions(row),
  },
];

/** rowKey identifies a certificate row by its id. */
function certificateRowKey(row: Certificate): string {
  return row.id;
}
</script>

<template>
  <NCard style="margin-top: 16px" title="Certificate configurations">
    <template #header-extra>
      <NText depth="3">one per application</NText>
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
        :scroll-x="1000"
        :pagination="{ pageSize: 10 }"
      />
      <NEmpty v-else description="No certificate configurations yet.">
        <template #extra>
          <NButton type="primary" @click="openCertificateCreate">
            Add certificate
          </NButton>
        </template>
      </NEmpty>
      <div class="embed">
        <h4>Status is observed live from the owning node</h4>
        <p>
          <span class="mono">present</span> means the node's ACME storage
          holds a certificate for the recorded domain and its expiry is
          shown; <span class="mono">absent</span> means the storage was
          read and holds none; <span class="mono">unknown</span> means the
          node could not be read. The status is computed on read — the
          control plane stores the desired configuration only.
        </p>
      </div>
    </NSpace>
    <template #footer>
      <NText depth="3">
        The recorded domain comes from the application's base domain at
        save time. Changing the base domain later requires saving the
        configuration again to re-record it.
      </NText>
    </template>
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

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
