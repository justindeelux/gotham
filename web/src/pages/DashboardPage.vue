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
import { computed, onMounted } from "vue";
import { RouterLink } from "vue-router";

import type { Server, ServerStatus } from "../api/servers";
import ServerStatusTag from "../components/ServerStatusTag.vue";
import { useServersStore } from "../stores/servers";
import { relativeTime, toPercent, USAGE_DANGER_PERCENT } from "../utils/format";

/**
 * Deployment is the typed seam for the future deployments store
 * (Phase 4). The recent-deploys widget renders an explicit empty state
 * until that store exists — never fabricated rows.
 *
 * TODO(phase-4): add web/src/stores/deployments.ts backed by the
 * deployments API, replace `deployments` below with live data, and
 * remove the empty state.
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

/** meterStatus maps usage to a Naive progress status (danger at the shared threshold). */
function meterStatus(
  value: number | null,
): "default" | "success" | "warning" | "error" {
  if (value === null || value === undefined) {
    return "default";
  }
  const percent = toPercent(value);
  if (percent >= USAGE_DANGER_PERCENT) {
    return "error";
  }
  if (percent >= 60) {
    return "warning";
  }
  return "success";
}

/** usageLabel renders a nullable usage reading as a percentage or dash. */
function usageLabel(value: number | null): string {
  if (value === null || value === undefined) {
    return "—";
  }
  return `${toPercent(value)}%`;
}

onMounted(() => {
  void serversStore.fetchServers().catch(() => {
    // The store already exposes the error; alert rendering is enough here.
  });
  serversStore.pollServers();
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
          Widgets without a backend show an explicit empty state until
          their phase lands.
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
        <NEmpty size="small" description="No applications yet — ships in Phase 4" />
      </NCard>

      <NCard class="kpi" title="Deploys in 24h" size="small">
        <NEmpty size="small" description="No deploys yet — ships in Phase 4" />
      </NCard>

      <NCard class="kpi" title="SSL certificates" size="small">
        <NEmpty size="small" description="No certificates yet — ships in Phase 6" />
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
            description="No deployments yet — ships in Phase 4"
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
              <NText depth="3">Build pipeline ships in Phase 4</NText>
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
                  <NText class="num metric-val">{{ usageLabel(server.cpu_usage) }}</NText>
                  <NProgress
                    type="line"
                    :percentage="toPercent(server.cpu_usage)"
                    :show-indicator="false"
                    :status="meterStatus(server.cpu_usage)"
                  />
                </div>
                <div class="node-metric">
                  <NText depth="3" class="metric-label">RAM</NText>
                  <NText class="num metric-val">{{ usageLabel(server.mem_usage) }}</NText>
                  <NProgress
                    type="line"
                    :percentage="toPercent(server.mem_usage)"
                    :show-indicator="false"
                    :status="meterStatus(server.mem_usage)"
                  />
                </div>
                <div class="node-metric">
                  <NText depth="3" class="metric-label">Disk</NText>
                  <NText class="num metric-val">{{ usageLabel(server.disk_usage) }}</NText>
                  <NProgress
                    type="line"
                    :percentage="toPercent(server.disk_usage)"
                    :show-indicator="false"
                    :status="meterStatus(server.disk_usage)"
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
                Postgres, Redis, and gateway health ships with a
                status endpoint in a later phase.
              </NText>
            </template>
          </NEmpty>
        </NCard>

        <NCard size="small" title="Team activity" class="aside-card">
          <NEmpty
            size="small"
            description="No team activity yet — ships in Phase 8"
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
