<script setup lang="ts">
import { NButton, NDataTable, NSpace, NText, type DataTableColumns } from "naive-ui";
import { h, type VNode } from "vue";

import type { Deployment } from "@/features/applications/api/applications";
import DeploymentStatusTag from "@/features/applications/components/DeploymentStatusTag.vue";
import { durationText } from "@/features/applications/utils/deploymentDuration";
import { relativeTime } from "@/shared/utils/format";

interface Props {
  deployments: Deployment[];
  loading?: boolean;
  paginated?: boolean;
}

const props = withDefaults(defineProps<Props>(), { loading: false, paginated: false });

const emit = defineEmits<{
  "show-logs": [deploymentId: string];
  "open-rollback": [deploymentId: string];
}>();

/** errorText renders the deployment error, falling back to an em dash. */
function errorText(deployment: Deployment): VNode {
  if (!deployment.error) {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  return h("span", { class: "mono error-text" }, deployment.error);
}

/** actionsCell renders per-row Logs / Rollback controls. */
function actionsCell(row: Deployment): VNode {
  const children: VNode[] = [
    h(
      NButton,
      {
        size: "small",
        quaternary: true,
        onClick: () => emit("show-logs", row.id),
      },
      { default: () => "Logs" },
    ),
  ];
  if (row.state === "running") {
    children.push(
      h(
        NButton,
        {
          size: "small",
          onClick: () => emit("open-rollback", row.id),
        },
        { default: () => "Rollback" },
      ),
    );
  }
  return h(NSpace, { size: 8, align: "center", wrap: false }, { default: () => children });
}

const columns: DataTableColumns<Deployment> = [
  {
    title: "Deploy",
    key: "id",
    width: 110,
    render: (row) => h("span", { class: "mono" }, row.id.slice(0, 8)),
  },
  {
    title: "Kind",
    key: "kind",
    width: 100,
    render: (row) => h("span", { class: "mono" }, row.kind),
  },
  {
    title: "Image",
    key: "image_tag",
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: (row) =>
      row.image_tag
        ? h("span", { class: "mono" }, row.image_tag)
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: "Duration",
    key: "duration",
    width: 90,
    render: (row) => h("span", { class: "tnum" }, durationText(row)),
  },
  {
    title: "State",
    key: "state",
    width: 130,
    render: (row) => h(DeploymentStatusTag, { state: row.state }),
  },
  {
    title: "Error",
    key: "error",
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: (row) => errorText(row),
  },
  {
    title: "Created",
    key: "created_at",
    width: 110,
    render: (row) => relativeTime(row.created_at),
  },
  {
    title: "Actions",
    key: "actions",
    width: 190,
    render: (row) => actionsCell(row),
  },
];

/** rowKey identifies a row by its deployment id. */
function rowKey(row: Deployment): string {
  return row.id;
}
</script>

<template>
  <NDataTable
    :columns="columns"
    :data="props.deployments"
    :loading="props.loading"
    :row-key="rowKey"
    :bordered="false"
    :scroll-x="1000"
    :pagination="props.paginated ? { pageSize: 10 } : false"
  />
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.tnum {
  font-variant-numeric: tabular-nums;
}

.error-text {
  color: var(--danger);
}
</style>
