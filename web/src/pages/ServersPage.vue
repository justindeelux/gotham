<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NIcon,
  NInput,
  NPopconfirm,
  NProgress,
  NSpace,
  NText,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h, onMounted, onUnmounted, ref } from "vue";
import type { VNode } from "vue";
import { useRouter } from "vue-router";

import { describeServerError } from "../api/servers";
import type { Server } from "../api/servers";
import AddServerWizard from "../components/AddServerWizard.vue";
import GothamIcon from "../components/GothamIcon.vue";
import ServerStatusTag from "../components/ServerStatusTag.vue";
import { useServersStore } from "../stores/servers";
import { relativeTime, toPercent } from "../utils/format";

const router = useRouter();
const serversStore = useServersStore();
const message = useMessage();

const wizardOpen = ref(false);
const validatingId = ref<string | null>(null);
const checkingAll = ref(false);

/** Filter chip keys mirroring the servers.html toolbar. */
type ServerFilter = "all" | "ready" | "offline" | "update";

const activeFilter = ref<ServerFilter>("all");
const searchQuery = ref("");

/** Meter value over which every usage bar turns danger red. */
const dangerThreshold = 80;

/**
 * needsAgentUpdate reports whether a server needs an agent update. The
 * control-plane API exposes no agent-version field on Server, so there is
 * currently no data to derive this from — the chip renders with a live count
 * of zero until the backend provides the signal. Never invented.
 */
function needsAgentUpdate(_server: Server): boolean {
  return false;
}

/** matchesFilter applies the active status chip to one server. */
function matchesFilter(server: Server, filter: ServerFilter): boolean {
  switch (filter) {
    case "ready":
      return server.status === "ready";
    case "offline":
      return server.status === "offline";
    case "update":
      return needsAgentUpdate(server);
    case "all":
    default:
      return true;
  }
}

/** matchesSearch applies the name/IP/OS query to one server. */
function matchesSearch(server: Server, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === "") {
    return true;
  }
  const haystacks = [server.name, server.ip, server.os ?? ""];
  return haystacks.some((field) => field.toLowerCase().includes(needle));
}

const readyCount = computed<number>(
  () => serversStore.servers.filter((server) => server.status === "ready").length,
);
const offlineCount = computed<number>(
  () => serversStore.servers.filter((server) => server.status === "offline").length,
);
const updateCount = computed<number>(
  () => serversStore.servers.filter((server) => needsAgentUpdate(server)).length,
);

/** filteredServers applies the chip filter and the search query client-side. */
const filteredServers = computed<Server[]>(() =>
  serversStore.servers.filter(
    (server) =>
      matchesFilter(server, activeFilter.value) &&
      matchesSearch(server, searchQuery.value),
  ),
);

/** meterColor picks the bar color: per-metric base, danger red over 80%. */
function meterColor(value: number, base: string): string {
  if (value > dangerThreshold) {
    return "var(--danger)";
  }
  return base;
}

/** usageCell renders a nullable usage reading as value + threshold bar.
 *
 * Heartbeat usage arrives as a fraction 0..1 (see toPercent), so the raw
 * reading is normalized before display and threshold coloring.
 */
function usageCell(value: number | null, baseColor: string): VNode {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  const rounded = toPercent(value);
  return h("div", { class: "metric" }, [
    h("span", { class: "metric-val" }, `${rounded}%`),
    h(NProgress, {
      type: "line",
      percentage: rounded,
      height: 6,
      color: meterColor(rounded, baseColor),
      "show-indicator": false,
      "border-radius": 9999,
    }),
  ]);
}

/** textCell renders a nullable string, falling back to an em dash. */
function textCell(value: string | null, mono = false): VNode {
  if (value === null || value === undefined || value === "") {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  if (mono) {
    return h("span", { class: "mono tnum" }, value);
  }
  return h("span", {}, value);
}

/**
 * actionsCell renders the per-row controls: re-validate (existing flow),
 * Open node (server-detail route), Containers (server-containers route), and
 * Delete with confirm. There is no agent-update backend route, so no Update
 * agent action is rendered — it is omitted, never faked.
 */
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
        { default: () => "Re-validate" },
      ),
      h(
        NButton,
        {
          size: "small",
          quaternary: true,
          onClick: () => {
            void openNode(row.id);
          },
        },
        { default: () => "Open node" },
      ),
      h(
        NButton,
        {
          size: "small",
          quaternary: true,
          onClick: () => {
            void openContainers(row.id);
          },
        },
        { default: () => "Containers" },
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
    render: (row) => h("span", { class: "mono tnum" }, `${row.ip}:${row.port}`),
  },
  {
    title: "Status",
    key: "status",
    width: 110,
    render: (row) => h(ServerStatusTag, { status: row.status }),
  },
  {
    title: "CPU",
    key: "cpu_usage",
    width: 140,
    render: (row) => usageCell(row.cpu_usage, "var(--accent)"),
  },
  {
    title: "RAM",
    key: "mem_usage",
    width: 140,
    render: (row) => usageCell(row.mem_usage, "var(--success)"),
  },
  {
    title: "Disk",
    key: "disk_usage",
    width: 140,
    render: (row) => usageCell(row.disk_usage, "var(--warn)"),
  },
  {
    title: "OS",
    key: "os",
    minWidth: 120,
    ellipsis: { tooltip: true },
    render: (row) => textCell(row.os),
  },
  {
    title: "Arch",
    key: "arch",
    width: 90,
    render: (row) => textCell(row.arch, true),
  },
  {
    title: "SSH user",
    key: "ssh_user",
    width: 100,
    render: (row) => textCell(row.ssh_user, true),
  },
  {
    title: "Containers",
    key: "container_count",
    width: 100,
    render: (row) =>
      row.container_count === null || row.container_count === undefined
        ? h(NText, { depth: 3 }, { default: () => "—" })
        : h("span", { class: "tnum" }, String(row.container_count)),
  },
  {
    title: "Docker",
    key: "docker_version",
    minWidth: 110,
    ellipsis: { tooltip: true },
    render: (row) => textCell(row.docker_version, true),
  },
  {
    title: "Last seen",
    key: "last_seen",
    width: 110,
    render: (row) => relativeTime(row.last_seen),
  },
  {
    title: "Actions",
    key: "actions",
    width: 320,
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

/**
 * handleCheckAll re-runs the SSH probes for every registered server using the
 * existing per-server validate flow, then reports a summary.
 */
async function handleCheckAll(): Promise<void> {
  if (checkingAll.value || serversStore.servers.length === 0) {
    return;
  }
  checkingAll.value = true;
  try {
    const results = await Promise.allSettled(
      serversStore.servers.map((server) => serversStore.validate(server.id)),
    );
    const failed = results.filter(
      (result) => result.status === "rejected" || !result.value.ok,
    ).length;
    const total = results.length;
    if (failed === 0) {
      message.success(`SSH check passed for ${total} ${total === 1 ? "node" : "nodes"}`);
    } else {
      message.error(`SSH check failed for ${failed} of ${total} nodes`);
    }
  } finally {
    checkingAll.value = false;
  }
}

/** openNode navigates to the server-detail route for one server. */
function openNode(id: string): Promise<void> {
  return router.push({ name: "server-detail", params: { id } }).then(() => undefined);
}

/** openContainers navigates to the server-containers route for one server. */
function openContainers(id: string): Promise<void> {
  return router.push({ name: "server-containers", params: { id } }).then(() => undefined);
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
  <div class="servers-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Operations · Node registry</p>
        <h1>Servers</h1>
        <p class="page-desc">
          Each node runs a <code class="inline-code">gotham-agent</code> connected to
          the gRPC gateway <span class="mono">:9442</span> over mTLS. The control
          plane never calls Docker directly — every command goes through the agent.
        </p>
      </div>
      <div class="page-actions">
        <NButton
          secondary
          :loading="checkingAll"
          :disabled="serversStore.servers.length === 0"
          @click="() => void handleCheckAll()"
        >
          Check SSH
        </NButton>
        <NButton type="primary" @click="wizardOpen = true">
          Add server
        </NButton>
      </div>
    </div>

    <NCard>
      <NAlert
        v-if="serversStore.error"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ serversStore.error }}
      </NAlert>

      <div class="toolbar">
        <div class="filters" role="group" aria-label="Filter servers by status">
          <button
            class="chip"
            type="button"
            :class="{ 'is-active': activeFilter === 'all' }"
            @click="activeFilter = 'all'"
          >
            All <span class="nav-count">{{ serversStore.servers.length }}</span>
          </button>
          <button
            class="chip"
            type="button"
            :class="{ 'is-active': activeFilter === 'ready' }"
            @click="activeFilter = 'ready'"
          >
            Ready <span class="nav-count">{{ readyCount }}</span>
          </button>
          <button
            class="chip"
            type="button"
            :class="{ 'is-active': activeFilter === 'offline' }"
            @click="activeFilter = 'offline'"
          >
            Offline <span class="nav-count">{{ offlineCount }}</span>
          </button>
          <button
            class="chip"
            type="button"
            :class="{ 'is-active': activeFilter === 'update' }"
            @click="activeFilter = 'update'"
          >
            Agent update needed <span class="nav-count">{{ updateCount }}</span>
          </button>
        </div>
        <NInput
          v-model:value="searchQuery"
          class="search-input"
          placeholder="Search by name, IP, OS…"
          aria-label="Search servers"
          clearable
        >
          <template #prefix>
            <NIcon>
              <GothamIcon name="search" />
            </NIcon>
          </template>
        </NInput>
      </div>

      <NDataTable
        v-if="filteredServers.length > 0 || serversStore.loading"
        :columns="columns"
        :data="filteredServers"
        :loading="serversStore.loading"
        :row-key="rowKey"
        :bordered="false"
        :scroll-x="1500"
        :pagination="{ pageSize: 10 }"
      />
      <NEmpty v-else class="servers-empty" description="No nodes match the current filters">
        <template #icon>
          <NIcon>
            <GothamIcon name="server" />
          </NIcon>
        </template>
        <template #extra>
          <p class="servers-empty-hint">Adjust the filters or add a new server over SSH.</p>
          <NButton type="primary" @click="wizardOpen = true">
            Add server
          </NButton>
        </template>
      </NEmpty>
    </NCard>

    <NCard class="install-card" title="Install the agent on a new node">
      <template #header-extra>
        <code class="inline-code">deploy/install-agent.sh</code>
      </template>
      <pre class="install-cmd"><code>curl -fsSL https://get.gotham.dev/install.sh | sudo sh -s -- \
  --cp-addr cp.gotham.dev:9442 \
  --node-id new-node-01

# The script downloads the arch-matched binary, verifies the checksum +
# Ed25519 signature, writes /etc/gotham/agent.crt issued by the
# control-plane CA, then enables the systemd unit.
systemctl status gotham-agent</code></pre>
      <div class="callouts">
        <div class="callout">
          <NIcon>
            <GothamIcon name="shield" />
          </NIcon>
          <div>
            <h4>Mutual mTLS</h4>
            <p>
              The control plane's internal CA signs a dedicated certificate for
              each agent at <code class="inline-code">Register</code> time. An
              agent only accepts commands from an authenticated control plane.
            </p>
          </div>
        </div>
        <div class="callout">
          <NIcon>
            <GothamIcon name="refresh" />
          </NIcon>
          <div>
            <h4>10-second heartbeat</h4>
            <p>
              Every heartbeat carries CPU, RAM, disk, and container count. Three
              missed beats in a row mark the node
              <span class="mono">offline</span> and raise an alert.
            </p>
          </div>
        </div>
      </div>
    </NCard>

    <AddServerWizard v-model:show="wizardOpen" />
  </div>
</template>

<style scoped>
.servers-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.page-head h1 {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.inline-code {
  font-family: var(--font-mono);
  font-size: 0.9em;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 1px 6px;
}

.mono {
  font-family: var(--font-mono);
}

.toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  margin-bottom: var(--space-4);
}

.filters {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-family: var(--font-body);
  font-size: var(--text-sm);
  color: var(--muted);
  background: transparent;
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 6px 14px;
  cursor: pointer;
  transition: background var(--motion-base) var(--ease-standard),
    color var(--motion-base) var(--ease-standard),
    border-color var(--motion-base) var(--ease-standard);
}

.chip:hover {
  background: var(--hover-row);
  color: var(--fg-2);
  border-color: var(--border-soft);
}

.chip.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
  border-color: var(--border-soft);
}

.chip .nav-count {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
}

.search-input {
  margin-left: auto;
  max-width: 280px;
}

.metric {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.metric-val {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  font-variant-numeric: tabular-nums;
  color: var(--fg);
}

.tnum {
  font-variant-numeric: tabular-nums;
}

.servers-empty {
  padding: var(--space-8) 0;
}

.servers-empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
}

.install-card {
  margin-top: var(--space-4);
}

.install-cmd {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  line-height: 1.6;
  color: var(--fg);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-4);
  margin: 0 0 var(--space-4);
  overflow-x: auto;
  white-space: pre;
}

.install-cmd code {
  font-family: inherit;
}

.callouts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-4);
}

.callout {
  display: flex;
  gap: var(--space-3);
  align-items: flex-start;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-4);
}

.callout h4 {
  font-size: var(--text-sm);
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.callout p {
  font-size: var(--text-sm);
  color: var(--muted);
  margin: 0;
}

@media (max-width: 860px) {
  .callouts {
    grid-template-columns: 1fr;
  }

  .page-actions {
    margin-left: 0;
    width: 100%;
  }

  .search-input {
    margin-left: 0;
    max-width: none;
    width: 100%;
  }
}
</style>
