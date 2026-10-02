<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NIcon,
  NInput,
  NPagination,
  NPopconfirm,
  NProgress,
  NSpin,
  useMessage,
} from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { describeServerError } from "../api/servers";
import type { Server } from "../api/servers";
import AddServerWizard from "../components/AddServerWizard.vue";
import GothamIcon from "../components/GothamIcon.vue";
import ServerStatusTag from "../components/ServerStatusTag.vue";
import { useServersStore } from "../stores/servers";
import { formatBytes, relativeTime, toPercent } from "../utils/format";

const router = useRouter();
const route = useRoute();
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

/**
 * Page size bounds the mounted cards. The store replaces the whole list every
 * five seconds, so rendering every node (three progress bars each) would make
 * the update cost grow without limit; the mockup has no pagination, so the
 * control stays a single row under the grid.
 */
const pageSize = 12;
const page = ref(1);

/** pageCount is the last page the filtered list has. */
const pageCount = computed<number>(() =>
  Math.max(1, Math.ceil(filteredServers.value.length / pageSize)),
);

/**
 * currentPage clamps the requested page to the available range: deleting the
 * only node on the last page (or the five-second refresh shrinking the list)
 * would otherwise leave an empty slice and hide a control that could return
 * the operator to the data.
 */
const currentPage = computed<number>(() => Math.min(page.value, pageCount.value));

/** pagedServers is the visible slice of the filtered list. */
const pagedServers = computed<Server[]>(() =>
  filteredServers.value.slice(
    (currentPage.value - 1) * pageSize,
    currentPage.value * pageSize,
  ),
);

// A filter or search change re-enters at the first page.
watch([activeFilter, searchQuery], () => {
  page.value = 1;
});

// Follow the list down when it shrinks under the current page.
watch(pageCount, (count) => {
  if (page.value > count) {
    page.value = count;
  }
});

/** meterColor picks the bar color: per-metric base, danger red over 80%. */
/**
 * initials builds the node avatar label: the first letters of up to two words
 * ("gotham-prod-01" -> "GP"). Ported from the server-detail avatar.
 */
function initials(name: string): string {
  const parts = name.split(/[-_.\s]+/).filter(Boolean);
  const letters = parts.slice(0, 2).map((part) => part[0] ?? "");
  return (letters.join("") || name.slice(0, 2)).toUpperCase();
}

/** MetricView is one rendered usage tile. */
interface MetricView {
  /** label is the displayed reading, or an em dash when the node reported none. */
  label: string;
  /** color is the bar color; danger red once the normalized reading exceeds 80%. */
  color: string;
  /** percentage is the normalized 0..100 reading the bar renders. */
  percentage: number;
}

/**
 * metricView normalizes a heartbeat usage fraction (0..1) before applying the
 * danger threshold — comparing the raw fraction with a percentage threshold
 * would never turn the bar red.
 */
function metricView(value: number | null, base: string): MetricView {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return { label: "—", color: base, percentage: 0 };
  }
  const percentage = Math.max(0, Math.min(100, toPercent(value)));
  return {
    label: `${percentage}%`,
    color: percentage > dangerThreshold ? "var(--danger)" : base,
    percentage,
  };
}

/** keyLabel identifies the SSH key by its short id; the API exposes no name. */
function keyLabel(server: Server): string {
  return server.ssh_key_id ? server.ssh_key_id.slice(0, 8) : "no key attached";
}

/** containerLabel keeps an unknown count (no heartbeat yet) distinct from zero. */
function containerLabel(server: Server): string {
  if (server.container_count === null || server.container_count === undefined) {
    return "—";
  }
  return `${server.container_count} container${server.container_count === 1 ? "" : "s"}`;
}

/**
 * nodeMeta is the head sub-line: address, OS and architecture. A non-default
 * SSH port is shown with the address (the table always did), so two nodes on
 * the same IP stay distinguishable.
 */
function nodeMeta(server: Server): string {
  const endpoint = server.port && server.port !== 22 ? `${server.ip}:${server.port}` : server.ip;
  return [endpoint, server.os ?? "—", server.arch ?? "—"].join(" \u00b7 ");
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

// The dashboard's "Add server" CTA deep-links here with ?add=1. Open the wizard
// once and strip the flag with a replace, so a refresh or a back navigation
// does not reopen it (B4-3).
watch(
  () => route.query.add,
  (value) => {
    if (value === "1") {
      wizardOpen.value = true;
      void router.replace({ name: "servers" });
    }
  },
  { immediate: true },
);
</script>

<template>
  <div class="servers-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Operations · Node registry</p>
        <h1>Servers</h1>
        <p class="page-desc">
          Each node runs a <code class="inline-code">gotham-agent</code> connected to
          the gRPC gateway <span class="mono">:9442</span> over server-authenticated
          TLS. The control plane never calls Docker directly — every command goes
          through the agent.
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

      <NSpin v-if="serversStore.loading && filteredServers.length === 0" class="servers-loading" />
      <template v-else-if="filteredServers.length > 0">
        <div class="grid cols-2 node-list" data-od-id="node-list">
        <article
          v-for="server in pagedServers"
          :key="server.id"
          class="node-card"
          :data-server="server.name"
        >
          <div class="node-head">
            <span class="avatar">{{ initials(server.name) }}</span>
            <div class="grow">
              <p class="fg-2 bold">{{ server.name }}</p>
              <p class="small muted">{{ nodeMeta(server) }}</p>
            </div>
            <ServerStatusTag :status="server.status" />
          </div>

          <dl class="kv">
            <dt>Docker</dt>
            <dd class="mono">{{ server.docker_version ?? "—" }}</dd>
            <dt>Resources</dt>
            <dd class="mono">
              {{ formatBytes(server.total_mem) }} RAM ·
              {{ formatBytes(server.total_disk) }} disk
            </dd>
            <dt>Agent</dt>
            <dd class="mono">
              {{ server.node_id ?? "—" }} · {{ relativeTime(server.last_seen) }}
            </dd>
            <dt>SSH</dt>
            <dd>
              <span class="inline-code">{{ keyLabel(server) }}</span> · user
              <span class="mono">{{ server.ssh_user }}</span>
            </dd>
          </dl>

          <div class="node-metrics">
            <div class="node-metric">
              <p class="stat-label">CPU</p>
              <p class="val">{{ metricView(server.cpu_usage, 'var(--accent)').label }}</p>
              <NProgress
                class="mt-2"
                type="line"
                :percentage="metricView(server.cpu_usage, 'var(--accent)').percentage"
                :color="metricView(server.cpu_usage, 'var(--accent)').color"
                :height="6"
                :show-indicator="false"
                :rail-style="{ borderRadius: 'var(--radius-pill)' }"
              />
            </div>
            <div class="node-metric">
              <p class="stat-label">RAM</p>
              <p class="val">{{ metricView(server.mem_usage, 'var(--success)').label }}</p>
              <NProgress
                class="mt-2"
                type="line"
                :percentage="metricView(server.mem_usage, 'var(--success)').percentage"
                :color="metricView(server.mem_usage, 'var(--success)').color"
                :height="6"
                :show-indicator="false"
                :rail-style="{ borderRadius: 'var(--radius-pill)' }"
              />
            </div>
            <div class="node-metric">
              <p class="stat-label">Disk</p>
              <p class="val">{{ metricView(server.disk_usage, 'var(--warn)').label }}</p>
              <NProgress
                class="mt-2"
                type="line"
                :percentage="metricView(server.disk_usage, 'var(--warn)').percentage"
                :color="metricView(server.disk_usage, 'var(--warn)').color"
                :height="6"
                :show-indicator="false"
                :rail-style="{ borderRadius: 'var(--radius-pill)' }"
              />
            </div>
          </div>

          <div class="node-foot">
            <span class="tag">{{ containerLabel(server) }}</span>
            <NButton size="small" style="margin-left: auto" @click="openNode(server.id)">
              Open node
            </NButton>
            <NButton
              size="small"
              :loading="validatingId === server.id"
              @click="handleValidate(server)"
            >
              Revalidate SSH
            </NButton>
            <NButton size="small" @click="openContainers(server.id)">
              Containers
            </NButton>
            <NPopconfirm @positive-click="handleDelete(server)">
              <template #trigger>
                <NButton size="small" type="error" secondary>Delete</NButton>
              </template>
              Remove {{ server.name }}? Containers on the node are not touched.
            </NPopconfirm>
          </div>
        </article>
        </div>
        <NPagination
          v-if="filteredServers.length > pageSize"
          class="servers-pagination"
          :page="currentPage"
          :page-size="pageSize"
          :item-count="filteredServers.length"
          @update:page="page = $event"
        />
      </template>
      <NEmpty
        v-else
        class="servers-empty"
        description="No nodes match the current filters"
      >
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
      <pre class="install-cmd"><code>scp root@&lt;cp-host&gt;:/var/lib/gotham/ca/ca.crt .
git clone --depth 1 https://github.com/justindeelux/gotham /tmp/gotham
sudo GOTHAM_AGENT_CP_ADDR=&lt;cp-host&gt;:9442 GOTHAM_AGENT_NODE_ID=&lt;node&gt; \
  /tmp/gotham/deploy/install-agent.sh --ca ./ca.crt

# The installer verifies the signed manifest + digest, writes
# /etc/gotham/ca.crt, then enables the systemd unit.
systemctl status gotham-agent</code></pre>
      <div class="callouts">
        <div class="callout">
          <NIcon>
            <GothamIcon name="shield" />
          </NIcon>
          <div>
            <h4>Server-authenticated TLS</h4>
            <p>
              The control plane signs its listener with its internal CA, and the
              agent verifies it against the copied <code class="inline-code">ca.crt</code>.
              An agent client certificate (mutual TLS) is planned.
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

/* ── Node grid (docs/design/servers.html) ──────────────────────────────────
   The list region is a grid of node cards, not a data table. These rules are
   ported from docs/design/assets/gotham-views.css (.node-card family) and
   docs/design/assets/gotham-ui.css (grid/avatar/tag/meter/kv utilities) that
   the app does not carry globally; tokens come from styles/tokens.css. */
.servers-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--space-4);
}

.grid {
  display: grid;
  gap: var(--space-4);
  margin-top: var(--space-4);
}

.cols-2 {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.node-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.node-card:hover {
  border-color: var(--border-soft);
}

.node-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.node-metrics {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-3);
}

.node-metric .stat-label {
  font-size: 10px;
}

.node-metric .val {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  color: var(--fg);
  margin-top: 2px;
}

.node-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  border-top: 1px solid var(--border);
  padding-top: var(--space-3);
}

.kv {
  display: grid;
  grid-template-columns: minmax(90px, 110px) minmax(0, 1fr);
  gap: var(--space-2) var(--space-4);
  align-items: baseline;
  margin: 0;
}

.kv dt {
  font-size: var(--text-xs);
  color: var(--muted);
}

.kv dd {
  margin: 0;
  font-size: var(--text-sm);
}

.avatar {
  width: 32px;
  height: 32px;
  border-radius: 10px;
  flex: 0 0 auto;
  display: grid;
  place-items: center;
  background: var(--accent);
  color: var(--accent-on);
  font-family: var(--font-display);
  font-size: var(--text-sm);
  font-weight: 700;
}

.tag {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border-radius: var(--radius-pill);
  font-family: var(--font-mono);
  font-size: 11px;
  background: var(--surface);
  border: 1px solid var(--border);
  color: var(--muted);
}

.stat-label {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--muted);
}

.inline-code {
  font-family: var(--font-mono);
  font-size: 0.92em;
  background: var(--surface-warm);
  border-radius: 3px;
  padding: 1px 5px;
  color: var(--fg);
}

.grow {
  flex: 1 1 auto;
  min-width: 0;
}

.fg-2 {
  color: var(--fg-2);
}

.bold {
  font-weight: 600;
}

.small {
  font-size: var(--text-xs);
}

.muted {
  color: var(--muted);
}

.mt-2 {
  margin-top: var(--space-2);
}

@media (max-width: 940px) {
  .cols-2 {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
