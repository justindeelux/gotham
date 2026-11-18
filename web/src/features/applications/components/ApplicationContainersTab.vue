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
import { computed, h } from "vue";
import type { VNode } from "vue";
import { useI18n } from "vue-i18n";

import type {
  ApplicationContainer,
  ApplicationContainerAction,
} from "@/features/applications/api/containers";
import { useApplicationContainers } from "@/features/applications/composables/useApplicationContainers";
import {
  isRunning,
  stateLabel,
  stateTagType,
} from "@/features/servers";

interface Props {
  appId: string;
  canWrite: boolean;
}

const props = defineProps<Props>();
const appId = computed<string>(() => props.appId);
const { locale, t } = useI18n();

const {
  containers,
  loading,
  loaded,
  error,
  isPending,
  isBusy,
  fetchContainers,
  runAction,
} = useApplicationContainers(appId);

const emptyDescription = computed<string>(() =>
  loaded.value
    ? String(t("applications.containers.emptyNone"))
    : String(t("applications.containers.emptyLoading")),
);

function stateCell(row: ApplicationContainer): VNode {
  return h(
    NTag,
    { type: stateTagType(row.state), size: "small", round: true },
    { default: () => stateLabel(row.state, locale.value) },
  );
}

function serviceCell(row: ApplicationContainer): VNode {
  return h(
    "span",
    { class: row.service ? "mono" : "muted" },
    row.service || "—",
  );
}

function actionButton(
  row: ApplicationContainer,
  action: ApplicationContainerAction,
  confirm: boolean,
): VNode {
  const label = String(t(`applications.containers.${action}`));
  const trigger = () =>
    h(
      NButton,
      {
        size: "small",
        secondary: true,
        loading: isPending(row.id, action),
        disabled: isBusy(row.id),
      },
      { default: () => label },
    );
  if (!confirm) {
    return h(
      NButton,
      {
        size: "small",
        secondary: true,
        loading: isPending(row.id, action),
        disabled: isBusy(row.id),
        onClick: () => void runAction(row, action),
      },
      { default: () => label },
    );
  }
  const message =
    action === "stop"
      ? t("applications.containers.stopConfirm", { name: row.name })
      : t("applications.containers.restartConfirm", { name: row.name });
  return h(
    NPopconfirm,
    {
      positiveText: label,
      negativeText: t("common.actions.cancel"),
      onPositiveClick: () => void runAction(row, action),
    },
    { trigger, default: () => message },
  );
}

function actionsCell(row: ApplicationContainer): VNode {
  const controls: VNode[] = [];
  if (isRunning(row.state)) {
    controls.push(actionButton(row, "restart", true));
    controls.push(actionButton(row, "stop", true));
  } else {
    controls.push(actionButton(row, "start", false));
  }
  return h(
    NSpace,
    { size: 8, align: "center", wrap: false },
    { default: () => controls },
  );
}

const columns = computed<DataTableColumns<ApplicationContainer>>(() => {
  const rows: DataTableColumns<ApplicationContainer> = [
    {
      title: t("applications.containers.tableName"),
      key: "name",
      minWidth: 180,
      ellipsis: { tooltip: true },
      render: (row) => h("span", { class: "mono" }, row.name || row.id),
    },
    {
      title: t("applications.containers.tableService"),
      key: "service",
      minWidth: 120,
      ellipsis: { tooltip: true },
      render: serviceCell,
    },
    {
      title: t("applications.containers.tableImage"),
      key: "image",
      minWidth: 180,
      ellipsis: { tooltip: true },
      render: (row) => h("span", { class: "mono muted" }, row.image || "—"),
    },
    {
      title: t("applications.containers.tableState"),
      key: "state",
      width: 130,
      render: stateCell,
    },
    {
      title: t("applications.containers.tableUptime"),
      key: "uptime",
      minWidth: 140,
      ellipsis: { tooltip: true },
      render: (row) =>
        row.status
          ? h("span", { class: "mono muted" }, row.status)
          : h(NText, { depth: 3 }, { default: () => "—" }),
    },
  ];
  if (props.canWrite) {
    rows.push({
      title: t("applications.containers.tableActions"),
      key: "actions",
      width: 190,
      render: actionsCell,
    });
  }
  return rows;
});

function rowKey(row: ApplicationContainer): string {
  return row.id;
}
</script>

<template>
  <NCard class="app-containers" :title="t('applications.containers.title')">
    <template #header-extra>
      <NButton
        size="small"
        secondary
        :loading="loading"
        @click="fetchContainers(true)"
      >
        {{ t("applications.containers.refresh") }}
      </NButton>
    </template>

    <NAlert
      v-if="error"
      type="error"
      :show-icon="true"
      class="app-containers-alert"
    >
      {{ error }}
    </NAlert>

    <NDataTable
      :columns="columns"
      :data="containers"
      :loading="loading"
      :row-key="rowKey"
      :bordered="false"
      :pagination="{ pageSize: 10 }"
    >
      <template #empty>
        <NEmpty :description="emptyDescription" />
      </template>
    </NDataTable>
  </NCard>
</template>

<style scoped>
.app-containers {
  margin-top: var(--space-4);
}

.app-containers-alert {
  margin-bottom: var(--space-3);
}
</style>
