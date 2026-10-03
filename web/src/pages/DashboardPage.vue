<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NProgress,
  NSkeleton,
  NSpace,
  NTag,
  NText,
} from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { RouterLink } from "vue-router";

import {
  countRunning,
  latestDeploymentStates,
  listApplications,
} from "../api/applications";
import type { Server, ServerStatus } from "../api/servers";
import ServerStatusTag from "../components/ServerStatusTag.vue";
import { useServersStore } from "../stores/servers";
import { applicationTileView } from "../utils/dashboard";
import { relativeTime, usageView } from "../utils/format";

/**
 * Deployment is the typed seam for a deployments store that does not exist
 * yet. The recent-deploys widget renders an explicit empty state until that
 * store exists — never fabricated rows.
 *
 * TODO: add web/src/stores/deployments.ts backed by the deployments API,
 * replace `deployments` below with live data, and remove the empty state.
 */
interface Deployment {
  id: string;
  appName: string;
  repo: string;
  branch: string;
  commit: string;
  trigger: string;
  duration: string;
  status: "running" | "failed" | "paused";
}

/** No backend serves deployments yet, so the list is always empty. */
const deployments: Deployment[] = [];

const serversStore = useServersStore();

/**
 * Applications backing the "Running applications" tile. Read once on mount
 * (no polling) from the caller's personal team — the same scope as the
 * applications page — with at most the latest deployment row per application
 * (GET ?limit=1, four requests at a time). The tile distinguishes four
 * states: loading, error (with a retry, never a false "none yet"),
 * genuinely empty, and ready. When only some per-application reads fail the
 * figure is marked incomplete ("at least N running") instead of falsely low.
 */
const applicationsLoading = ref(true);
const applicationsError = ref<string | null>(null);
/** Per-application latest-state reads that failed; >0 marks the tile incomplete. */
const applicationFailedReads = ref(0);
const applicationTotal = ref(0);
const applicationRunning = ref(0);

/** fetchApplicationCounts loads the personal-team list plus latest states. */
async function fetchApplicationCounts(): Promise<void> {
  applicationsLoading.value = true;
  applicationsError.value = null;
  try {
    const applications = await listApplications();
    applicationTotal.value = applications.length;
    if (applications.length === 0) {
      applicationRunning.value = 0;
      applicationFailedReads.value = 0;
      return;
    }
    const { states, failed } = await latestDeploymentStates(
      applications.map((application) => application.id),
    );
    applicationRunning.value = countRunning(states);
    applicationFailedReads.value = failed;
  } catch {
    applicationTotal.value = 0;
    applicationRunning.value = 0;
    applicationFailedReads.value = 0;
    applicationsError.value = "Could not load applications";
  } finally {
    applicationsLoading.value = false;
  }
}

/**
 * tile is the pure render decision for the "Running applications" tile
 * (loading / error / empty / incomplete / ready, plus the "≥N/total" text).
 * The template switches on tile.state, so every branch is pinned by the
 * ui-truth harness through applicationTileView.
 */
const tile = computed(() =>
  applicationTileView({
    loading: applicationsLoading.value,
    error: applicationsError.value,
    total: applicationTotal.value,
    running: applicationRunning.value,
    failedReads: applicationFailedReads.value,
  }),
);

const servers = computed<Server[]>(() => serversStore.servers);
const readyCount = computed<number>(
  () => servers.value.filter((server) => server.status === "ready").length,
);
const totalCount = computed<number>(() => servers.value.length);

const offlineServers = computed<Server[]>(() =>
  servers.value.filter(
    (server) => server.status === "offline" || server.status === "error",
  ),
);

/**
 * aggregateStatus summarizes the fleet for the KPI badge. It never claims
 * "ready" while readyCount is 0: a fleet that is entirely pending/validating
 * reads "pending", and an unreachable node reads "offline" (B4-12).
 */
const aggregateStatus = computed<ServerStatus>(() => {
  if (offlineServers.value.length > 0) {
    return "offline";
  }
  if (readyCount.value > 0) {
    return "ready";
  }
  return "pending";
});

/** Bound the widgets so a large fleet is not re-diffed in full every 5s (B4-14). */
const nodeWidgetLimit = 6;
const heartbeatWidgetLimit = 8;
const visibleNodes = computed<Server[]>(() =>
  servers.value.slice(0, nodeWidgetLimit),
);
const hiddenNodeCount = computed<number>(() =>
  Math.max(0, servers.value.length - nodeWidgetLimit),
);
const visibleHeartbeats = computed<Server[]>(() =>
  servers.value.slice(0, heartbeatWidgetLimit),
);
const hiddenHeartbeatCount = computed<number>(() =>
  Math.max(0, servers.value.length - heartbeatWidgetLimit),
);

/** initials derives a two-letter node avatar from the server name. */
function initials(name: string): string {
  const parts = name.replace(/[^a-zA-Z0-9]+/g, " ").trim().split(/\s+/);
  if (parts.length === 1) {
    return name.slice(0, 2).toUpperCase();
  }
  return (parts[0][0] + parts[1][0]).toUpperCase();
}

/** nodeSubtitle summarizes address, OS, and Docker version. */
function nodeSubtitle(server: Server): string {
  const bits = [`${server.ip}:${server.port}`];
  if (server.os) {
    bits.push(server.os);
  }
  if (server.docker_version) {
    bits.push(`Docker ${server.docker_version}`);
  }
  return bits.join(" · ");
}

onMounted(() => {
  void serversStore.fetchServers().catch(() => {
    // The store already exposes the error; alert rendering is enough here.
  });
  serversStore.pollServers();
  void fetchApplicationCounts();
});
</script>

<template>
  <div class="dash">
    <div class="page-head">
      <div>
        <p class="eyebrow">Overview</p>
        <h1>Dashboard</h1>
        <p class="page-desc">
          One control plane for every node, application, and database.
        </p>
      </div>
      <div class="page-actions">
        <RouterLink :to="{ name: 'servers' }" custom>
          <template #default="{ navigate }">
            <NButton quaternary @click="navigate">View servers</NButton>
          </template>
        </RouterLink>
        <!-- The add-server wizard lives on the servers page (no dedicated
             route), so this links there with a forward-compatible flag. -->
        <RouterLink :to="{ name: 'servers', query: { add: '1' } }" custom>
          <template #default="{ navigate }">
            <NButton type="primary" @click="navigate">Add server</NButton>
          </template>
        </RouterLink>
      </div>
    </div>

    <div v-if="serversStore.error" class="dash-alert">
      <NAlert type="error" :show-icon="true">
        {{ serversStore.error }}
      </NAlert>
    </div>

    <div class="kpi-row">
      <NCard class="kpi kpi--live" title="Servers ready" size="small">
        <NSkeleton v-if="serversStore.loading && totalCount === 0" text :repeat="2" />
        <template v-else>
          <p class="kpi-value num">
            {{ readyCount }}<span class="kpi-unit">/{{ totalCount }}</span>
          </p>
          <p class="kpi-sub">
            <ServerStatusTag v-if="totalCount > 0" :status="aggregateStatus" />
            <NText v-else depth="3">No servers yet — add one to begin.</NText>
            <NText v-if="offlineServers.length > 0" depth="3">
              {{ offlineServers.map((server) => server.name).join(", ") }}
              unreachable
            </NText>
          </p>
        </template>
      </NCard>

      <NCard class="kpi" title="Running applications" size="small">
        <NSkeleton v-if="tile.state === 'loading'" text :repeat="2" />
        <template v-else-if="tile.state === 'error'">
          <p class="kpi-value num">—</p>
          <p class="kpi-sub">
            <NText depth="3">{{ tile.error }}</NText>
            <NButton
              size="small"
              quaternary
              @click="() => void fetchApplicationCounts()"
            >
              Retry
            </NButton>
          </p>
        </template>
        <template v-else-if="tile.state === 'ready'">
          <p class="kpi-value num">
            <span v-if="tile.incomplete" aria-hidden="true">≥</span
            >{{ tile.running }}<span class="kpi-unit">/{{ tile.total }}</span>
          </p>
          <p class="kpi-sub">
            <RouterLink :to="{ name: 'applications' }">View applications</RouterLink>
            <NText v-if="tile.incomplete" depth="3">
              Some states could not be read
            </NText>
          </p>
        </template>
        <NEmpty v-else size="small" description="No applications yet" />
      </NCard>

      <NCard class="kpi" title="Deploys in 24h" size="small">
        <NEmpty size="small" description="No deploy data yet" />
      </NCard>

      <NCard class="kpi" title="SSL certificates" size="small">
        <NEmpty size="small" description="No certificate data yet" />
      </NCard>
    </div>

    <div class="dash-split">
      <div class="dash-main">
        <div class="section-title">
          <h2>Recent deploys</h2>
          <NText depth="3" class="mono meta">source: deployments · realtime via Redis</NText>
        </div>
        <NCard size="small">
          <NEmpty
            v-if="deployments.length === 0"
            description="No deployments yet"
          >
            <template #extra>
              <NText depth="3">
                Push an application to see build history, durations, and
                statuses here.
              </NText>
            </template>
          </NEmpty>
          <NSpace vertical :size="12">
            <div class="card-foot">
              <NText depth="3">Queue: no data yet</NText>
              <NText depth="3">No build history to show</NText>
            </div>
          </NSpace>
        </NCard>

        <div class="section-title">
          <h2>Server health</h2>
          <NText depth="3" class="mono meta">
            heartbeat every 10s over gRPC server-authenticated TLS
          </NText>
        </div>
        <NEmpty
          v-if="!serversStore.loading && totalCount === 0"
          description="No servers yet"
        >
          <template #extra>
            <NSpace vertical :size="8" align="center">
              <NText depth="3">
                Add your first node to see CPU, RAM, and disk health here.
              </NText>
              <RouterLink :to="{ name: 'servers', query: { add: '1' } }" custom>
                <template #default="{ navigate }">
                  <NButton type="primary" size="small" @click="navigate">
                    Add server
                  </NButton>
                </template>
              </RouterLink>
            </NSpace>
          </template>
        </NEmpty>
        <div v-else class="node-grid">
          <NSkeleton v-if="serversStore.loading && totalCount === 0" text :repeat="3" />
          <NCard
            v-for="server in visibleNodes"
            :key="server.id"
            size="small"
            class="node-card"
          >
            <NSpace vertical :size="12">
              <NSpace align="center" :size="12" :wrap="false">
                <div class="node-avatar" aria-hidden="true">
                  {{ initials(server.name) }}
                </div>
                <div class="node-head">
                  <NText strong>{{ server.name }}</NText>
                  <NText depth="3" class="node-sub">{{ nodeSubtitle(server) }}</NText>
                </div>
                <ServerStatusTag :status="server.status" />
              </NSpace>
              <div class="node-metrics">
                <div class="node-metric">
                  <NText depth="3" class="metric-label">CPU</NText>
                  <NText class="num metric-val">
                    {{ usageView(server.cpu_usage, "var(--accent)").label }}
                  </NText>
                  <NProgress
                    type="line"
                    :percentage="usageView(server.cpu_usage, 'var(--accent)').percentage"
                    :show-indicator="false"
                    :color="usageView(server.cpu_usage, 'var(--accent)').color"
                  />
                </div>
                <div class="node-metric">
                  <NText depth="3" class="metric-label">RAM</NText>
                  <NText class="num metric-val">
                    {{ usageView(server.mem_usage, "var(--success)").label }}
                  </NText>
                  <NProgress
                    type="line"
                    :percentage="usageView(server.mem_usage, 'var(--success)').percentage"
                    :show-indicator="false"
                    :color="usageView(server.mem_usage, 'var(--success)').color"
                  />
                </div>
                <div class="node-metric">
                  <NText depth="3" class="metric-label">Disk</NText>
                  <NText class="num metric-val">
                    {{ usageView(server.disk_usage, "var(--success)").label }}
                  </NText>
                  <NProgress
                    type="line"
                    :percentage="usageView(server.disk_usage, 'var(--success)').percentage"
                    :show-indicator="false"
                    :color="usageView(server.disk_usage, 'var(--success)').color"
                  />
                </div>
              </div>
              <NSpace align="center" :size="8">
                <NTag v-if="server.container_count !== null" size="small" :bordered="false">
                  {{ server.container_count }} containers
                </NTag>
                <NTag v-if="server.arch" size="small" :bordered="false">
                  {{ server.arch }}
                </NTag>
                <NTag v-if="server.ssh_user" size="small" :bordered="false">
                  {{ server.ssh_user }}
                </NTag>
              </NSpace>
            </NSpace>
          </NCard>
        </div>
        <!-- Cap the grid so a large fleet is not re-diffed in full every 5s
             (B4-14); link out for the rest. -->
        <NText v-if="hiddenNodeCount > 0" depth="3" class="meta">
          +{{ hiddenNodeCount }} more node{{ hiddenNodeCount === 1 ? "" : "s" }} —
          <RouterLink :to="{ name: 'servers' }">view all servers</RouterLink>
        </NText>
      </div>

      <aside class="dash-aside">
        <NCard size="small" title="Heartbeat" class="aside-card">
          <template #header-extra>
            <NTag size="small" type="success" :bordered="false">
              <span class="pulse-dot" aria-hidden="true" />live
            </NTag>
          </template>
          <NEmpty
            v-if="totalCount === 0"
            size="small"
            description="No heartbeats yet — add a server"
          />
          <NSpace v-else vertical :size="8">
            <div
              v-for="server in visibleHeartbeats"
              :key="server.id"
              class="heartbeat-row"
            >
              <NText depth="2">{{ server.name }}</NText>
              <NText depth="3" class="mono">{{ relativeTime(server.last_seen) }}</NText>
            </div>
            <NText v-if="hiddenHeartbeatCount > 0" depth="3" class="meta">
              +{{ hiddenHeartbeatCount }} more node{{ hiddenHeartbeatCount === 1 ? "" : "s" }}
            </NText>
          </NSpace>
          <template #footer>
            <NText depth="3" class="mono meta">10s cycle · Heartbeat(stream) in agent.v1</NText>
          </template>
        </NCard>

        <NCard size="small" title="Control-plane components" class="aside-card">
          <NEmpty
            size="small"
            description="No component telemetry yet"
          >
            <template #extra>
              <NText depth="3">
                Database, cache, and gateway health is not reported yet.
              </NText>
            </template>
          </NEmpty>
        </NCard>

        <NCard size="small" title="Team activity" class="aside-card">
          <NEmpty
            size="small"
            description="No team activity yet"
          />
        </NCard>

        <NCard size="small" title="Alerts" class="aside-card">
          <NSpace v-if="offlineServers.length > 0" vertical :size="8">
            <NAlert
              v-for="server in offlineServers"
              :key="server.id"
              type="error"
              :show-icon="true"
              :title="`${server.name} unreachable`"
            >
              Agent heartbeat lost. Last seen
              {{ relativeTime(server.last_seen) }}.
            </NAlert>
          </NSpace>
          <NEmpty v-else size="small" description="No alerts — all nodes healthy" />
        </NCard>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.dash {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  max-width: 1360px;
}

.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
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
  font-size: var(--text-sm);
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.page-actions {
  display: flex;
  gap: var(--space-2);
  align-items: center;
}

.dash-alert {
  max-width: 720px;
}

.kpi-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-4);
}

.kpi-value {
  font-size: var(--text-3xl);
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.kpi-unit {
  font-size: var(--text-lg);
  color: var(--muted);
}

.kpi-sub {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin: 0;
  flex-wrap: wrap;
}

.dash-split {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 360px);
  gap: var(--space-5);
  align-items: start;
}

.dash-main {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-width: 0;
}

.section-title {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.section-title h2 {
  font-size: var(--text-xl);
  color: var(--fg-2);
  margin: 0;
}

.mono {
  font-family: var(--font-mono);
}

.num {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}

.meta {
  font-size: var(--text-xs);
  color: var(--muted);
}

.card-foot {
  display: flex;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.node-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-4);
}

.node-avatar {
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-md);
  background: var(--accent-softer);
  color: var(--accent-ink);
  font-weight: 700;
  font-size: var(--text-sm);
}

.node-head {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1 1 auto;
}

.node-sub {
  font-size: var(--text-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-metrics {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-3);
}

.node-metric {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.metric-label {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.metric-val {
  font-size: var(--text-sm);
  color: var(--fg-2);
}

.dash-aside {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-width: 0;
}

.heartbeat-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  font-size: var(--text-sm);
}

.pulse-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
  background: var(--success);
  margin-right: 6px;
  animation: dash-pulse var(--motion-base) var(--ease-standard) infinite alternate;
}

@keyframes dash-pulse {
  from {
    opacity: 1;
  }
  to {
    opacity: 0.45;
  }
}

@media (max-width: 1180px) {
  .dash-split {
    grid-template-columns: minmax(0, 1fr);
  }
  .kpi-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .kpi-row {
    grid-template-columns: minmax(0, 1fr);
  }
  .node-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
