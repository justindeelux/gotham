<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NDrawer,
  NDrawerContent,
  NEmpty,
  NSpace,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h, onMounted, onUnmounted, ref, watch } from "vue";
import type { HTMLAttributes, VNode } from "vue";
import { RouterLink, useRoute } from "vue-router";

import type { Container, ContainerAction } from "../api/containers";
import {
  describeContainerError,
  listContainers,
  restartContainer,
  startContainer,
  stopContainer,
} from "../api/containers";
import { getServer } from "../api/servers";
import { useMediaQuery } from "../composables/useMediaQuery";
import LogViewer from "../components/LogViewer.vue";

/** Polling cadence for the container list, in milliseconds. */
const pollIntervalMs = 5_000;

const route = useRoute();
const message = useMessage();

const serverId = computed<string>(() => String(route.params.id ?? ""));

const containers = ref<Container[]>([]);
const loading = ref(false);
const error = ref<string | null>(null);
const serverName = ref<string>("");
const loaded = ref(false);

/** Per-action in-flight markers keyed by `${containerId}:${action}`. */
const pending = ref<Record<string, boolean>>({});

const selected = ref<Container | null>(null);
const drawerOpen = ref(false);

/** isNarrow tracks viewports where the fixed log drawer would overflow. */
const isNarrow = useMediaQuery("(max-width: 760px)");

/** drawerWidth keeps the log drawer inside narrow viewports. */
const drawerWidth = computed<number | string>(() =>
  isNarrow.value ? "94vw" : 720,
);

let pollTimer: ReturnType<typeof setInterval> | null = null;

/** Tag type for a raw Docker lifecycle state. */
function stateTagType(state: string): "default" | "success" | "warning" | "error" {
  switch (state.trim().toLowerCase()) {
    case "running":
      return "success";
    case "restarting":
    case "paused":
    case "created":
      return "warning";
    case "exited":
    case "dead":
      return "error";
    default:
      return "default";
  }
}

/** isRunning reports whether a container is in the running state. */
function isRunning(state: string): boolean {
  return state.trim().toLowerCase() === "running";
}

/** isPending reports whether one action is in flight for a container. */
function isPending(containerId: string, action: ContainerAction): boolean {
  return pending.value[`${containerId}:${action}`] === true;
}

/** isBusy reports whether any action is in flight for a container. */
function isBusy(containerId: string): boolean {
  return Object.keys(pending.value).some((key) =>
    key.startsWith(`${containerId}:`),
  );
}

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
      loading: isPending(row.id, action),
      disabled: isBusy(row.id),
      onClick: (event: MouseEvent) => {
        event.stopPropagation();
        void runAction(row, action, label);
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
        openLogs(row);
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

/** rowProps makes the whole row clickable to open the log drawer. */
function rowProps(row: Container): HTMLAttributes {
  return {
    style: "cursor: pointer;",
    onClick: () => openLogs(row),
  };
}

/** openLogs selects a container and opens the log drawer. */
function openLogs(row: Container): void {
  selected.value = row;
  drawerOpen.value = true;
}

/** fetchContainers reloads the list; `showLoading` drives the table spinner. */
async function fetchContainers(showLoading: boolean): Promise<void> {
  if (!serverId.value) {
    return;
  }
  if (showLoading) {
    loading.value = true;
  }
  try {
    containers.value = await listContainers(serverId.value);
    error.value = null;
    loaded.value = true;
  } catch (err) {
    error.value = describeContainerError(err);
  } finally {
    loading.value = false;
  }
}

/** fetchServer loads the server name for the heading; failures are ignored. */
async function fetchServer(): Promise<void> {
  if (!serverId.value) {
    return;
  }
  try {
    const server = await getServer(serverId.value);
    serverName.value = server.name;
  } catch {
    // The breadcrumb falls back to the server id when the lookup fails.
  }
}

/** runAction invokes one lifecycle action and refreshes the list on success. */
async function runAction(
  row: Container,
  action: ContainerAction,
  label: string,
): Promise<void> {
  const key = `${row.id}:${action}`;
  pending.value = { ...pending.value, [key]: true };
  try {
    if (action === "start") {
      await startContainer(serverId.value, row.id);
    } else if (action === "stop") {
      await stopContainer(serverId.value, row.id);
    } else {
      await restartContainer(serverId.value, row.id);
    }
    message.success(`${label} requested for ${row.name}`);
    await fetchContainers(false);
  } catch (err) {
    message.error(describeContainerError(err));
  } finally {
    const next = { ...pending.value };
    delete next[key];
    pending.value = next;
  }
}

/** startPolling refreshes the container list in place every few seconds. */
function startPolling(): void {
  if (pollTimer !== null) {
    return;
  }
  pollTimer = setInterval(() => {
    void fetchContainers(false);
  }, pollIntervalMs);
}

/** stopPolling clears the refresh interval. */
function stopPolling(): void {
  if (pollTimer !== null) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

/** initialize loads the server name and the container list. */
async function initialize(): Promise<void> {
  loaded.value = false;
  await Promise.all([fetchServer(), fetchContainers(true)]);
}

watch(serverId, () => {
  selected.value = null;
  drawerOpen.value = false;
  void initialize();
});

onMounted(() => {
  void initialize();
  startPolling();
});

onUnmounted(() => {
  stopPolling();
});
</script>

<template>
  <NSpace vertical :size="16">
    <nav class="breadcrumb" aria-label="Breadcrumb">
      <RouterLink to="/servers">Servers</RouterLink>
      <span class="breadcrumb__sep">/</span>
      <span class="muted">{{ serverName || serverId }}</span>
    </nav>

    <NCard>
      <template #header>
        <NSpace align="center" justify="space-between">
          <NSpace align="center" :size="10">
            <NText strong>Containers</NText>
            <NText v-if="serverName" depth="3">{{ serverName }}</NText>
          </NSpace>
          <NButton secondary :loading="loading" @click="fetchContainers(true)">
            Refresh
          </NButton>
        </NSpace>
      </template>

      <NAlert
        v-if="error"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ error }}
      </NAlert>

      <NDataTable
        :columns="columns"
        :data="containers"
        :loading="loading"
        :row-key="rowKey"
        :row-props="rowProps"
        :bordered="false"
        :scroll-x="960"
        :pagination="{ pageSize: 10 }"
      >
        <template #empty>
          <NEmpty
            :description="
              loaded ? 'No containers on this node.' : 'Loading containers…'
            "
          />
        </template>
      </NDataTable>
    </NCard>

    <NDrawer v-model:show="drawerOpen" :width="drawerWidth" placement="right">
      <NDrawerContent closable :native-scrollbar="false">
        <LogViewer
          v-if="selected"
          :server-id="serverId"
          :container-id="selected.id"
          :title="selected.name"
          :subtitle="selected.image"
          :auto-start-stream="true"
        />
      </NDrawerContent>
    </NDrawer>
  </NSpace>
</template>

<style scoped>
.breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
}

.breadcrumb__sep {
  color: var(--meta);
}
</style>
