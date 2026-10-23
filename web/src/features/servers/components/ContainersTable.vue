<script setup lang="ts">
import {
  NButton,
  NDataTable,
  NEmpty,
  NSpace,
  NTag,
  NText,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h } from "vue";
import type { HTMLAttributes, VNode } from "vue";
import { useI18n } from "vue-i18n";

import type { Container, ContainerAction } from "@/features/servers/api/containers";
import {
  isRunning,
  stateLabel,
  stateTagType,
} from "@/features/servers/utils/containerView";

interface Props {
  containers: Container[];
  loading: boolean;
  emptyDescription: string;
  isPending: (_containerId: string, _action: ContainerAction) => boolean;
  isBusy: (_containerId: string) => boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  action: [row: Container, action: ContainerAction, label: string];
  openLogs: [row: Container];
}>();

const { locale, t } = useI18n();

function stateCell(row: Container): VNode {
  const title = row.status || row.state || t("servers.containers.unknown");
  return h("span", { title }, [
    h(
      NTag,
      { type: stateTagType(row.state), size: "small", round: true },
      { default: () => stateLabel(row.state, locale.value) },
    ),
  ]);
}

function portsCell(row: Container): VNode {
  if (!row.ports || row.ports.length === 0) {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  return h("span", { class: "mono" }, row.ports.join(", "));
}

function statCell(): VNode {
  const note = t("servers.containers.notReported");
  return h(
    "span",
    {
      class: "mono muted",
      title: note,
      "aria-label": note,
    },
    "—",
  );
}

function uptimeCell(row: Container): VNode {
  return h("span", { class: "mono muted" }, row.status || "—");
}

function actionButton(
  row: Container,
  action: ContainerAction,
): VNode {
  const label = t(`servers.containers.${action}`);
  return h(
    NButton,
    {
      size: "small",
      secondary: true,
      loading: props.isPending(row.id, action),
      disabled: props.isBusy(row.id),
      onClick: (event: MouseEvent) => {
        event.stopPropagation();
        emit("action", row, action, label);
      },
    },
    { default: () => label },
  );
}

function logsButton(row: Container): VNode {
  const label = t("servers.containers.logs");
  return h(
    NButton,
    {
      size: "small",
      quaternary: true,
      onClick: (event: MouseEvent) => {
        event.stopPropagation();
        emit("openLogs", row);
      },
    },
    { default: () => label },
  );
}

function actionsCell(row: Container): VNode {
  const controls: VNode[] = [];
  if (isRunning(row.state)) {
    controls.push(actionButton(row, "restart"));
    controls.push(actionButton(row, "stop"));
  } else {
    controls.push(actionButton(row, "start"));
  }
  controls.push(logsButton(row));
  return h(NSpace, { size: 8, align: "center", wrap: false }, {
    default: () => controls,
  });
}

const columns = computed<DataTableColumns<Container>>(() => [
  {
    title: t("servers.containers.tableName"),
    key: "name",
    minWidth: 180,
    ellipsis: { tooltip: true },
    render: (row) => h("span", { class: "mono" }, row.name || row.id),
  },
  {
    title: t("servers.containers.tableImage"),
    key: "image",
    minWidth: 180,
    ellipsis: { tooltip: true },
    render: (row) =>
      h("span", { class: "mono muted" }, row.image || "—"),
  },
  {
    title: t("servers.containers.tableState"),
    key: "state",
    width: 140,
    render: (row) => stateCell(row),
  },
  {
    title: t("servers.containers.tablePorts"),
    key: "ports",
    minWidth: 140,
    render: (row) => portsCell(row),
  },
  {
    title: t("servers.containers.tableCpu"),
    key: "cpu",
    width: 80,
    render: () => statCell(),
  },
  {
    title: t("servers.containers.tableRam"),
    key: "ram",
    width: 90,
    render: () => statCell(),
  },
  {
    title: t("servers.containers.tableUptime"),
    key: "uptime",
    minWidth: 140,
    ellipsis: { tooltip: true },
    render: (row) => uptimeCell(row),
  },
  {
    title: t("servers.containers.tableActions"),
    key: "actions",
    width: 220,
    render: (row) => actionsCell(row),
  },
]);

function rowKey(row: Container): string {
  return row.id;
}

function rowProps(row: Container): HTMLAttributes {
  return {
    style: "cursor: pointer;",
    onClick: () => emit("openLogs", row),
  };
}
</script>

<template>
  <NDataTable
    :columns="columns"
    :data="containers"
    :loading="loading"
    :row-key="rowKey"
    :row-props="rowProps"
    :bordered="false"
    :scroll-x="1200"
    :pagination="{ pageSize: 10 }"
  >
    <template #empty>
      <NEmpty :description="emptyDescription" />
    </template>
  </NDataTable>
</template>
