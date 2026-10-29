<script setup lang="ts">
import {
  NButton,
  NDataTable,
  NPopconfirm,
  NSpace,
  NTag,
  NText,
  type DataTableColumns,
} from "naive-ui";
import { computed, h } from "vue";
import { useI18n } from "vue-i18n";

import type { ApplicationDomain } from "@/features/applications/api/applications";
import {
  certificateStatusLabel,
  certificateStatusTagType,
  type Certificate,
} from "@/features/domains";
import DomainHostCell from "@/features/domains/components/DomainHostCell.vue";

interface Props {
  rows: ApplicationDomain[];
  loading: boolean;
  busyDomainId: string | null;
  certificateOf: (_host: string) => Certificate | null;
  diagnosticOf: (_host: string) => string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  edit: [row: ApplicationDomain];
  "set-primary": [row: ApplicationDomain];
  certificate: [row: ApplicationDomain];
  remove: [row: ApplicationDomain];
}>();

const { t } = useI18n();

function removeConfirm(row: ApplicationDomain): string {
  return props.certificateOf(row.domain)
    ? t("applications.domain.removeWithCertConfirm", { domain: row.domain })
    : t("applications.domain.removeConfirm", { domain: row.domain });
}

const columns = computed<DataTableColumns<ApplicationDomain>>(() => [
  {
    title: t("applications.domain.host"),
    key: "domain",
    minWidth: 220,
    render: (row) => {
      const diagnostic = props.diagnosticOf(row.domain);
      return h(DomainHostCell, { host: row.domain, reason: diagnostic });
    },
  },
  {
    title: t("applications.domain.port"),
    key: "port",
    width: 90,
    render: (row) =>
      h(
        "span",
        { class: "mono" },
        row.port > 0 ? String(row.port) : t("applications.domain.portAuto"),
      ),
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
      const cert = props.certificateOf(row.domain);
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
    fixed: "right",
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
                "aria-label": t("applications.domain.editAria", { domain: row.domain }),
                loading: props.busyDomainId === row.id,
                onClick: () => emit("edit", row),
              },
              { default: () => t("applications.domain.edit") },
            ),
            row.is_primary
              ? null
              : h(
                  NButton,
                  {
                    size: "small",
                    "aria-label": t("applications.domain.setPrimaryAria", {
                      domain: row.domain,
                    }),
                    disabled: row.disabled,
                    loading: props.busyDomainId === row.id,
                    onClick: () => emit("set-primary", row),
                  },
                  { default: () => t("applications.domain.setPrimary") },
                ),
            h(
              NButton,
              {
                size: "small",
                class: "domain-certificate",
                "aria-label": t("applications.domain.certificateAria", {
                  domain: row.domain,
                }),
                onClick: () => emit("certificate", row),
              },
              { default: () => t("applications.domain.certificate") },
            ),
            h(
              NPopconfirm,
              {
                onPositiveClick: () => emit("remove", row),
              },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      size: "small",
                      type: "error",
                      ghost: true,
                      "aria-label": t("applications.domain.removeAria", {
                        domain: row.domain,
                      }),
                      loading: props.busyDomainId === row.id,
                    },
                    { default: () => t("applications.domain.remove") },
                  ),
                default: () => removeConfirm(row),
              },
            ),
          ],
        },
      ),
  },
]);

function rowProps(): Record<string, unknown> {
  return { class: "domain-row" };
}
</script>

<template>
  <NDataTable
    class="domain-table"
    :columns="columns"
    :data="props.rows"
    :loading="props.loading"
    :row-key="(row: ApplicationDomain) => row.id"
    :row-props="rowProps"
    :bordered="false"
    :single-line="false"
    :scroll-x="1120"
    :pagination="false"
  />
</template>

<style scoped>
.domain-table {
  width: 100%;
}

.domain-table :deep(.domain-row) {
  vertical-align: middle;
}

.domain-table :deep(.n-data-table-th) {
  white-space: nowrap;
}
</style>
