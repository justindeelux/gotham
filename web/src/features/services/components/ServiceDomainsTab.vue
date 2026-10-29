<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NFormItem,
  NInputNumber,
  NPopconfirm,
  NSelect,
  NSpace,
  NSwitch,
  NTag,
  NText,
  type DataTableColumns,
} from "naive-ui";
import { computed, h, onMounted, watch } from "vue";

import type {
  ServiceDomain,
  ServiceDomainProtocol,
  ServiceDomainStatus,
} from "@/features/services/api/services";
import { useServiceDomains } from "@/features/services/composables/useServiceDomains";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import DomainFormModal from "@/features/domains/components/DomainFormModal.vue";
import DomainHostCell from "@/features/domains/components/DomainHostCell.vue";
import { activeLocale, i18n } from "@/shared/i18n";

interface Props {
  /** Incremented by the page when the overview asks to add a domain. */
  createTicket?: number;
}

const props = withDefaults(defineProps<Props>(), { createTicket: 0 });

const emit = defineEmits<{
  "open-compose": [];
  "create-consumed": [];
}>();

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const { service, serviceId, canWrite } = useServiceDetailContext();
const domains = useServiceDomains(service, serviceId, canWrite);

const protocolOptions = computed<
  Array<{ label: string; value: ServiceDomainProtocol }>
>(() => [
  { label: t("services.domains.protocolHttp"), value: "http" },
  { label: t("services.domains.protocolHttps"), value: "https" },
]);

function statusType(
  status: ServiceDomainStatus,
): "success" | "warning" | "default" {
  if (status === "routed") {
    return "success";
  }
  if (status === "held" || status === "shadowed") {
    return "warning";
  }
  return "default";
}

function rowKey(row: ServiceDomain): string {
  return (
    row.id ??
    `compose:${row.compose_service}:${row.domain}:${row.port}:${row.protocol}`
  );
}

const columns = computed<DataTableColumns<ServiceDomain>>(() => {
  const actions = {
    title: t("services.domains.actions"),
    key: "actions",
    width: 200,
    fixed: "right" as const,
    render: (row: ServiceDomain) => {
      if (row.source === "compose" || row.id === undefined) {
        return h(
          NButton,
          {
            size: "small",
            class: "domain-open-compose",
            onClick: () => emit("open-compose"),
          },
          { default: () => t("services.domains.openCompose") },
        );
      }
      if (!canWrite.value) {
        return h(NText, { depth: 3 }, { default: () => "—" });
      }
      return h(
        NSpace,
        { size: 8, wrap: false, align: "center" },
        {
          default: () => [
            h(
              NButton,
              {
                size: "small",
                "aria-label": t("services.domains.editAria", {
                  domain: row.domain,
                }),
                loading: domains.busyDomainId.value === row.id,
                onClick: () => domains.startEdit(row),
              },
              { default: () => t("services.domains.edit") },
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => void domains.handleRemove(row) },
              {
                trigger: () =>
                  h(
                    NButton,
                    {
                      size: "small",
                      type: "error",
                      ghost: true,
                      "aria-label": t("services.domains.removeAria", {
                        domain: row.domain,
                      }),
                      loading: domains.busyDomainId.value === row.id,
                    },
                    { default: () => t("services.domains.remove") },
                  ),
                default: () =>
                  t("services.domains.removeConfirm", { domain: row.domain }),
              },
            ),
          ],
        },
      );
    },
  };

  return [
    {
      title: t("services.domains.host"),
      key: "domain",
      minWidth: 240,
      render: (row) =>
        h(DomainHostCell, {
          host: row.domain,
          reason: row.reason ?? "",
          statusLabel: domains.statusLabel(row),
          statusType: statusType(row.status),
          badge:
            row.source === "compose"
              ? t("services.domains.fromCompose")
              : undefined,
        }),
    },
    {
      title: t("services.domains.target"),
      key: "compose_service",
      minWidth: 120,
      render: (row) =>
        h("span", { class: "mono" }, row.compose_service),
    },
    {
      title: t("services.domains.port"),
      key: "port",
      width: 90,
      render: (row) => h("span", { class: "mono" }, String(row.port)),
    },
    {
      title: t("services.domains.protocol"),
      key: "protocol",
      width: 110,
      render: (row) =>
        h(
          NTag,
          {
            size: "small",
            type: row.protocol === "https" ? "success" : "default",
          },
          { default: () => row.protocol },
        ),
    },
    {
      title: t("services.domains.enabled"),
      key: "enabled",
      width: 110,
      render: (row) => {
        if (row.source === "compose" || row.id === undefined) {
          return h(NText, { depth: 3 }, {
            default: () => t("services.domains.notEnabled"),
          });
        }
        if (!canWrite.value) {
          return h(
            NTag,
            { size: "small", type: row.disabled ? "warning" : "success" },
            {
              default: () =>
                row.disabled
                  ? t("services.domains.disabled")
                  : t("services.domains.enabledOn"),
            },
          );
        }
        return h(NSwitch, {
          value: !row.disabled,
          size: "small",
          loading: domains.busyDomainId.value === row.id,
          "aria-label": t("services.domains.toggleAria", { domain: row.domain }),
          "onUpdate:value": (value: boolean) => void domains.handleToggle(row, value),
        });
      },
    },
    actions,
  ];
});

onMounted(() => {
  if (props.createTicket > 0) {
    domains.openCreate();
    emit("create-consumed");
  }
});

watch(
  () => props.createTicket,
  () => {
    if (props.createTicket > 0) {
      domains.openCreate();
      emit("create-consumed");
    }
  },
);
</script>

<template>
  <NCard :title="t('services.domains.title')">
    <template #header-extra>
      <NButton
        v-if="canWrite"
        type="primary"
        size="small"
        class="domain-add"
        @click="domains.openCreate"
      >
        {{ t("services.domains.add") }}
      </NButton>
    </template>
    <NSpace vertical :size="12">
      <NText depth="3">{{ t("services.domains.hint") }}</NText>
      <NAlert v-if="domains.error.value" type="error" :show-icon="true">
        {{ domains.error.value }}
      </NAlert>

      <NDataTable
        v-if="domains.domains.value.length > 0 || domains.loading.value"
        class="domain-table"
        :columns="columns"
        :data="domains.domains.value"
        :loading="domains.loading.value"
        :row-key="rowKey"
        :bordered="false"
        :single-line="false"
        :scroll-x="900"
        :pagination="false"
      />
      <NEmpty v-else :description="t('services.domains.empty')">
        <template v-if="canWrite" #extra>
          <NButton size="small" class="domain-add-empty" @click="domains.openCreate">
            {{ t("services.domains.add") }}
          </NButton>
        </template>
      </NEmpty>
    </NSpace>

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
      :enabled="domains.draft.enabled"
      :saving="domains.saving.value"
      :error="domains.formError.value"
      :host-error="domains.errors.domain"
      :hint="t('services.domains.portHint')"
      @update:host="domains.draft.domain = $event"
      @update:protocol="domains.draft.protocol = $event"
      @update:enabled="domains.draft.enabled = $event"
      @save="domains.handleSave"
      @cancel="domains.closeForm"
    >
      <template #routing>
        <NFormItem :label="t('services.domains.target')" class="domain-target">
          <NSelect
            :value="domains.draft.target || null"
            class="domain-service"
            :options="
              domains.targets.value.map((target) => ({
                label: target.service,
                value: target.service,
              }))
            "
            :placeholder="t('services.domains.targetPlaceholder')"
            :aria-label="t('services.domains.targetAria')"
            @update:value="domains.handleTargetChange"
          />
        </NFormItem>
        <NFormItem :label="t('services.domains.port')" class="domain-routing-field">
          <NInputNumber
            :value="domains.draft.port"
            class="domain-port"
            :min="1"
            :max="65535"
            :placeholder="t('services.domains.portPlaceholder')"
            :input-props="{ 'aria-label': t('services.domains.portAria') }"
            @update:value="(value: number | null) => (domains.draft.port = value)"
          />
        </NFormItem>
      </template>
    </DomainFormModal>
  </NCard>
</template>

<style scoped>
.domain-target {
  flex: 2 1 0;
  min-width: 0;
}

.domain-service,
.domain-port {
  width: 100%;
}

.domain-routing-field {
  flex: 1;
  min-width: 0;
}

.domain-table :deep(.n-data-table-th) {
  white-space: nowrap;
}
</style>
