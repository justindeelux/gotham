<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NPopconfirm,
  NProgress,
  NSpace,
  NText,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { h, onMounted, onUnmounted, ref } from "vue";
import type { VNode } from "vue";

import { describeServerError } from "../api/servers";
import type { Server } from "../api/servers";
import AddServerWizard from "../components/AddServerWizard.vue";
import ServerStatusTag from "../components/ServerStatusTag.vue";
import { useServersStore } from "../stores/servers";
import { relativeTime } from "../utils/format";

const serversStore = useServersStore();
const message = useMessage();

const wizardOpen = ref(false);
const validatingId = ref<string | null>(null);

/** usageCell renders a nullable percentage as a progress bar. */
function usageCell(value: number | null): VNode {
  if (value === null || value === undefined) {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  return h(NProgress, {
    type: "line",
    percentage: Math.round(Math.min(Math.max(value, 0), 100)),
    height: 14,
  });
}

/** actionsCell renders the per-row validate and delete controls. */
function actionsCell(row: Server): VNode {
  return h(NSpace, { size: 8, align: "center", wrap: false }, {
    default: () => [
      h(
        NButton,
        {
          size: "small",
          loading: validatingId.value === row.id,
          onClick: () => {
            void handleValidate(row);
          },
        },
        { default: () => "Validate" },
      ),
      h(
        NPopconfirm,
        {
          onPositiveClick: () => {
            void handleDelete(row);
          },
        },
        {
          trigger: () =>
            h(
              NButton,
              { size: "small", type: "error", quaternary: true },
              { default: () => "Delete" },
            ),
          default: () => `Delete server "${row.name}"?`,
        },
      ),
    ],
  });
}

const columns: DataTableColumns<Server> = [
  { title: "Name", key: "name", minWidth: 140, ellipsis: { tooltip: true } },
  {
    title: "Address",
    key: "address",
    minWidth: 150,
    render: (row) => `${row.ip}:${row.port}`,
  },
  {
    title: "Status",
    key: "status",
    width: 120,
    render: (row) => h(ServerStatusTag, { status: row.status }),
  },
  {
    title: "CPU",
    key: "cpu_usage",
    width: 140,
    render: (row) => usageCell(row.cpu_usage),
  },
  {
    title: "RAM",
    key: "mem_usage",
    width: 140,
    render: (row) => usageCell(row.mem_usage),
  },
  {
    title: "Disk",
    key: "disk_usage",
    width: 140,
    render: (row) => usageCell(row.disk_usage),
  },
  {
    title: "Docker",
    key: "docker_version",
    minWidth: 120,
    render: (row) => row.docker_version ?? "—",
  },
  {
    title: "Last seen",
    key: "last_seen",
    width: 120,
    render: (row) => relativeTime(row.last_seen),
  },
  {
    title: "Actions",
    key: "actions",
    width: 190,
    render: (row) => actionsCell(row),
  },
];

/** rowKey identifies a row by its server id. */
function rowKey(row: Server): string {
  return row.id;
}

/** handleValidate probes one server and reports the outcome. */
async function handleValidate(server: Server): Promise<void> {
  validatingId.value = server.id;
  try {
    const outcome = await serversStore.validate(server.id);
    if (outcome.ok) {
      message.success(`${server.name}: validation passed`);
      return;
    }
    const failed = outcome.checks
      .filter((check) => !check.ok)
      .map((check) => check.name)
      .join(", ");
    message.error(
      outcome.message || `${server.name}: failed checks: ${failed}`,
    );
  } catch (error) {
    message.error(describeServerError(error));
  } finally {
    validatingId.value = null;
  }
}

/** handleDelete removes one server after the popconfirm is accepted. */
async function handleDelete(server: Server): Promise<void> {
  try {
    await serversStore.removeServer(server.id);
    message.success(`Deleted ${server.name}`);
  } catch (error) {
    message.error(describeServerError(error));
  }
}

onMounted(() => {
  void serversStore.fetchServers().catch(() => {
    // The store already exposes the error; message rendering is enough here.
  });
  serversStore.pollServers();
});

onUnmounted(() => {
  serversStore.stopPolling();
});
</script>

<template>
  <NSpace vertical :size="16">
    <NCard>
      <template #header>
        <NSpace align="center" justify="space-between">
          <NText strong>Servers</NText>
          <NButton type="primary" @click="wizardOpen = true">
            Add server
          </NButton>
        </NSpace>
      </template>

      <NAlert
        v-if="serversStore.error"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ serversStore.error }}
      </NAlert>

      <NDataTable
        :columns="columns"
        :data="serversStore.servers"
        :loading="serversStore.loading"
        :row-key="rowKey"
        :bordered="false"
        :pagination="{ pageSize: 10 }"
      />
    </NCard>

    <AddServerWizard v-model:show="wizardOpen" />
  </NSpace>
</template>
