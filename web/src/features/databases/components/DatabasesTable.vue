<script setup lang="ts">
import {
  NButton,
  NDataTable,
  NPopconfirm,
  NSpace,
  NTag,
  NText,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { h } from "vue";
import type { VNode } from "vue";

import type { Database } from "@/features/databases/api/databases";
import DatabaseStatusTag from "@/features/databases/components/DatabaseStatusTag.vue";
import { engineLabel } from "@/features/databases/utils/databaseFilters";
import { useDatabasesStore } from "@/features/databases/stores/databases";
import { useServersStore } from "@/features/servers";
import { relativeTime } from "@/shared/utils/format";

interface Props {
  databases: Database[];
  loading: boolean;
  restartingId: string | null;
}

interface Emits {
  details: [database: Database];
  restart: [database: Database];
  delete: [database: Database];
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const databasesStore = useDatabasesStore();
const serversStore = useServersStore();

/** serverName resolves a node id to its display name. */
function serverName(serverId: string): string {
  return (
    serversStore.servers.find((server) => server.id === serverId)?.name ??
    serverId.slice(0, 8)
  );
}

/** nameCell renders the database name with its engine image. */
function nameCell(database: Database): VNode {
  return h("div", { class: "cell-main" }, [
    h("span", { class: "mono cell-name" }, database.name),
    h("span", { class: "cell-sub mono" }, engineLabel(database)),
  ]);
}

/** portCell renders the public port tag or an em dash. */
function portCell(database: Database): VNode {
  if (database.public_port > 0) {
    return h(NTag, { size: "small", round: true }, {
      default: () => String(database.public_port),
    });
  }
  return h(NText, { depth: 3 }, { default: () => "—" });
}

/** actionsCell renders per-row Details / restart / delete controls. */
function actionsCell(database: Database): VNode {
  return h(NSpace, { size: 8, align: "center", wrap: false }, {
    default: () => [
      h(
        NButton,
        {
          size: "small",
          secondary: true,
          onClick: () => emit("details", database),
        },
        { default: () => "Details" },
      ),
      h(
        NButton,
        {
          size: "small",
          secondary: true,
          loading: props.restartingId === database.id,
          disabled: databasesStore.acting,
          "aria-label": `Restart ${database.name}`,
          onClick: () => emit("restart", database),
        },
        { default: () => "Restart" },
      ),
      h(
        NPopconfirm,
        {
          onPositiveClick: () => emit("delete", database),
        },
        {
          trigger: () =>
            h(
              NButton,
              { size: "small", type: "error", ghost: true, "aria-label": `Delete ${database.name}` },
              { default: () => "Delete" },
            ),
          default: () =>
            `Delete database "${database.name}"? The container is removed from the node, the volume ${database.volume} is kept for 7 days.`,
        },
      ),
    ],
  });
}

const columns: DataTableColumns<Database> = [
  {
    title: "Database",
    key: "name",
    minWidth: 200,
    render: (row) => nameCell(row),
  },
  {
    title: "Node",
    key: "server_id",
    width: 160,
    render: (row) => h("span", { class: "mono" }, serverName(row.server_id)),
  },
  {
    title: "Public port",
    key: "public_port",
    width: 120,
    render: (row) => portCell(row),
  },
  {
    title: "Status",
    key: "status",
    width: 130,
    render: (row) => h(DatabaseStatusTag, { status: row.status }),
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
    width: 280,
    render: (row) => actionsCell(row),
  },
];

/** rowKey identifies a row by its database id. */
function rowKey(row: Database): string {
  return row.id;
}
</script>

<template>
  <NDataTable
    :columns="columns"
    :data="databases"
    :loading="loading"
    :row-key="rowKey"
    :bordered="false"
    :scroll-x="900"
    :pagination="{ pageSize: 10 }"
  />
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.cell-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.cell-name {
  color: var(--fg-2);
  font-weight: 600;
}

.cell-sub {
  font-size: var(--text-xs);
  color: var(--muted);
}
</style>
