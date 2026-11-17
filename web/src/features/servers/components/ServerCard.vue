<script setup lang="ts">
import { NButton, NPopconfirm, NProgress } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";

import { useAuthStore } from "@/features/auth";
import type { AgentUpdateInfo, Server } from "@/features/servers/api/servers";
import ServerStatusTag from "@/features/servers/components/ServerStatusTag.vue";
import {
  agentUpdateInFlight,
  agentVersionMismatch,
  containerLabel,
  initials,
  keyLabel,
  needsAgentUpdate,
  nodeMeta,
} from "@/features/servers/utils/serverListView";
import { formatBytes, relativeTime, toPercent, usageLevel, usageView } from "@/shared/utils/format";

interface Props {
  server: Server;
  validating: boolean;
  agentUpdate?: AgentUpdateInfo;
  agentTargetVersion?: string | null;
  controlPlaneVersion?: string | null;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  open: [];
  edit: [];
  validate: [];
  containers: [];
  delete: [];
  "update-agent": [];
}>();

const { locale, t } = useI18n();
const authStore = useAuthStore();
const diskHigh = computed(() => usageLevel(toPercent(props.server.disk_usage)) !== "ok");
const canUpdate = computed<boolean>(() =>
  needsAgentUpdate(props.server, props.agentTargetVersion),
);
const versionMismatch = computed<boolean>(() =>
  agentVersionMismatch(props.server, props.controlPlaneVersion),
);
const updateRunning = computed<boolean>(() => agentUpdateInFlight(props.agentUpdate?.state));
const agentStatus = computed<string>(() => {
  if (updateRunning.value) {
    return t("servers.card.updatingAgent");
  }
  switch (props.agentUpdate?.state) {
    case "failed":
      return t("servers.card.agentUpdateFailed");
    case "up_to_date":
      return t("servers.card.agentUpToDate");
    case "pending":
      return canUpdate.value ? t("servers.card.agentUpdatePending") : "";
    default:
      return canUpdate.value
        ? t("servers.card.agentUpdateNeeded")
        : versionMismatch.value
          ? t("servers.card.agentVersionMismatch")
          : "";
  }
});
</script>

<template>
  <article class="node-card" :data-server="server.name">
    <div class="node-head">
      <span class="avatar">{{ initials(server.name) }}</span>
      <div class="grow">
        <p class="fg-2 bold">{{ server.name }}</p>
        <p class="small muted">{{ nodeMeta(server) }}</p>
      </div>
      <ServerStatusTag :status="server.status" />
    </div>

    <dl class="kv">
      <dt>{{ $t("servers.card.docker") }}</dt>
      <dd class="mono">{{ server.docker_version ?? "—" }}</dd>
      <dt>{{ $t("servers.card.resources") }}</dt>
      <dd class="mono">
        {{ formatBytes(server.total_mem) }} RAM ·
        {{ formatBytes(server.total_disk) }} disk
      </dd>
      <dt>{{ $t("servers.card.agent") }}</dt>
      <dd>
        <div class="agent-line">
          <span class="mono">{{ server.agent_version ?? $t("servers.list.unknownAgent") }}</span>
          <span v-if="controlPlaneVersion" class="muted small">
            · {{ $t("servers.card.controlPlaneVersion", { version: controlPlaneVersion }) }}
          </span>
        </div>
        <div class="agent-line muted small">
          {{ server.node_id ?? "—" }} · {{ relativeTime(server.last_seen) }}
        </div>
        <div v-if="agentStatus" class="agent-state">
          <span
            class="tag"
            :class="{
              'tag-warn': canUpdate || versionMismatch || agentUpdate?.state === 'failed',
              'tag-ok': agentUpdate?.state === 'up_to_date',
            }"
          >
            {{ agentStatus }}
          </span>
          <span v-if="agentUpdate?.state === 'failed' && agentUpdate.detail" class="agent-detail">
            {{ agentUpdate.detail }}
          </span>
        </div>
      </dd>
      <dt>{{ $t("servers.card.ssh") }}</dt>
      <dd>
        <span class="inline-code">{{ keyLabel(server, locale) }}</span
        ><template v-if="server.ssh_user">
          · {{ $t("servers.card.sshUserWord") }} <span class="mono">{{ server.ssh_user }}</span>
        </template>
      </dd>
    </dl>

    <div class="node-metrics">
      <div class="node-metric">
        <p class="stat-label">CPU</p>
        <p class="val">{{ usageView(server.cpu_usage, 'var(--accent)').label }}</p>
        <NProgress
          class="mt-2"
          type="line"
          :percentage="usageView(server.cpu_usage, 'var(--accent)').percentage"
          :color="usageView(server.cpu_usage, 'var(--accent)').color"
          :height="6"
          :show-indicator="false"
          :rail-style="{ borderRadius: 'var(--radius-pill)' }"
        />
      </div>
      <div class="node-metric">
        <p class="stat-label">RAM</p>
        <p class="val">{{ usageView(server.mem_usage, 'var(--success)').label }}</p>
        <NProgress
          class="mt-2"
          type="line"
          :percentage="usageView(server.mem_usage, 'var(--success)').percentage"
          :color="usageView(server.mem_usage, 'var(--success)').color"
          :height="6"
          :show-indicator="false"
          :rail-style="{ borderRadius: 'var(--radius-pill)' }"
        />
      </div>
      <div class="node-metric">
        <p class="stat-label">Disk</p>
        <p class="val">{{ usageView(server.disk_usage, 'var(--success)').label }}</p>
        <NProgress
          class="mt-2"
          type="line"
          :percentage="usageView(server.disk_usage, 'var(--success)').percentage"
          :color="usageView(server.disk_usage, 'var(--success)').color"
          :height="6"
          :show-indicator="false"
          :rail-style="{ borderRadius: 'var(--radius-pill)' }"
        />
        <RouterLink
          v-if="authStore.isOwner && diskHigh"
          class="node-metric__cleanup"
          :to="{ name: 'cleanup', query: { server: server.id } }"
        >
          {{ $t("servers.card.cleanUp") }}
        </RouterLink>
      </div>
    </div>

    <div class="node-foot">
      <span class="tag">{{ containerLabel(server, locale) }}</span>
      <NButton
        v-if="canUpdate"
        size="small"
        type="primary"
        secondary
        :loading="updateRunning"
        :disabled="updateRunning"
        @click="emit('update-agent')"
      >
        {{ $t("servers.card.updateAgent") }}
      </NButton>
      <NButton size="small" style="margin-left: auto" @click="emit('open')">
        {{ $t("servers.card.openNode") }}
      </NButton>
      <NButton size="small" @click="emit('edit')">
        {{ $t("servers.card.edit") }}
      </NButton>
      <NButton
        size="small"
        :loading="validating"
        @click="emit('validate')"
      >
        {{ $t("servers.card.revalidateSsh") }}
      </NButton>
      <NButton size="small" @click="emit('containers')">
        {{ $t("servers.card.containers") }}
      </NButton>
      <NPopconfirm @positive-click="emit('delete')">
        <template #trigger>
          <NButton size="small" type="error" secondary>{{ $t("servers.card.delete") }}</NButton>
        </template>
        {{ $t("servers.card.deleteConfirm", { name: server.name }) }}
      </NPopconfirm>
    </div>
  </article>
</template>

<style scoped>
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

.node-metric__cleanup {
  display: inline-block;
  margin-top: var(--space-2);
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--warn-ink, var(--warn));
  text-decoration: none;
}

.node-metric__cleanup:hover,
.node-metric__cleanup:focus-visible {
  text-decoration: underline;
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

.agent-line {
  display: flex;
  align-items: baseline;
  gap: var(--space-1);
  flex-wrap: wrap;
}

.agent-state {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  flex-wrap: wrap;
  margin-top: var(--space-1);
}

.agent-detail {
  min-width: 0;
  overflow-wrap: anywhere;
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

.tag-warn {
  background: var(--warn-soft);
  border-color: transparent;
  color: var(--warn-ink);
}

.tag-ok {
  background: var(--success-soft);
  border-color: transparent;
  color: var(--success-ink);
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

.mono {
  font-family: var(--font-mono);
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
</style>
