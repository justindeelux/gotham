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
import { h } from "vue";
import type { HTMLAttributes, VNode } from "vue";

import type { Container, ContainerAction } from "@/features/servers/api/containers";
import {
  isRunning,
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

/** stateCell renders the container's lifecycle state as a coloured tag. */
function stateCell(row: Container): VNode {
  const title = row.status || row.state || "unknown";
  return h("span", { title }, [
    h(
      NTag,
      { type: stateTagType(row.state), size: "small", round: true },
      { default: () => row.state || "unknown" },
    ),
  ]);
}

/** portsCell renders the declared port mappings, or an em dash when empty. */
function portsCell(row: Container): VNode {
  if (!row.ports || row.ports.length === 0) {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  return h("span", { class: "mono" }, row.ports.join(", "));
}

/**
 * statCell renders a per-container metric. The agent ContainerInfo contract
 * carries no CPU/RAM reading yet, so the mockup's columns render an explicit
 * em dash rather than a fabricated value. The aria-label keeps the dash from
 * reading as punctuation alone.
 */
function statCell(): VNode {
  return h(
    "span",
    {
      class: "mono muted",
      title: "Not reported by the agent yet",
      "aria-label": "Not reported by the agent yet",
    },
    "—",
  );
}

/** uptimeCell renders Docker's human uptime/status string for the row. */
function uptimeCell(row: Container): VNode {
  return h("span", { class: "mono muted" }, row.status || "—");
}

/** actionButton renders one start/stop/restart control for a row. */
function actionButton(
  row: Container,
  action: ContainerAction,
  label: string,
): VNode {
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

/** logsButton opens the log drawer without triggering the row click. */
function logsButton(row: Container): VNode {
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
    { default: () => "Logs" },
  );
}

/** actionsCell renders the per-row lifecycle controls. */
function actionsCell(row: Container): VNode {
  const controls: VNode[] = [];
  if (isRunning(row.state)) {
    controls.push(actionButton(row, "restart", "Restart"));
    controls.push(actionButton(row, "stop", "Stop"));
  } else {
    controls.push(actionButton(row, "start", "Start"));
  }
  controls.push(logsButton(row));
  return h(NSpace, { size: 8, align: "center", wrap: false }, {
    default: () => controls,
  });
}

const columns: DataTableColumns<Container> = [
  {
    title: "Name",
    key: "name",
    minWidth: 180,
    ellipsis: { tooltip: true },
    render: (row) => h("span", { class: "mono" }, row.name || row.id),
  },
  {
    title: "Image",
    key: "image",
    minWidth: 180,
    ellipsis: { tooltip: true },
    render: (row) =>
      h("span", { class: "mono muted" }, row.image || "—"),
  },
  {
    title: "State",
    key: "state",
    width: 140,
    render: (row) => stateCell(row),
  },
  {
    title: "Ports",
    key: "ports",
    minWidth: 140,
    render: (row) => portsCell(row),
  },
  {
    title: "CPU",
    key: "cpu",
    width: 80,
    render: () => statCell(),
  },
  {
    title: "RAM",
    key: "ram",
    width: 90,
    render: () => statCell(),
  },
  {
    title: "Uptime",
    key: "uptime",
    minWidth: 140,
    ellipsis: { tooltip: true },
    render: (row) => uptimeCell(row),
  },
  {
    title: "Actions",
    key: "actions",
    width: 220,
    render: (row) => actionsCell(row),
  },
];

/** rowKey identifies a row by its container id. */
function rowKey(row: Container): string {
  return row.id;
}

/**
 * rowProps makes the whole row clickable for the mouse. There is deliberately
 * no keyboard affordance on the row: every row already has a focusable Logs
 * button, and a keydown handler on the row would double-fire when focus sits
 * on an inner Start/Stop/Restart/Logs button (while swallowing Space's native
 * activation). A `role="button"` would be invalid here for the same reason —
 * a button must not contain interactive descendants.
 */
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
