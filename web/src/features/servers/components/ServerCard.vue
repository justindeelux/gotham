<script setup lang="ts">
import { NButton, NPopconfirm, NProgress } from "naive-ui";

import type { Server } from "@/features/servers/api/servers";
import ServerStatusTag from "@/features/servers/components/ServerStatusTag.vue";
import {
  containerLabel,
  initials,
  keyLabel,
  nodeMeta,
} from "@/features/servers/utils/serverListView";
import { formatBytes, relativeTime, usageView } from "@/shared/utils/format";

interface Props {
  server: Server;
  validating: boolean;
}

defineProps<Props>();
const emit = defineEmits<{
  open: [];
  edit: [];
  validate: [];
  containers: [];
  delete: [];
}>();
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
        <span class="inline-code">{{ keyLabel(server) }}</span
        ><template v-if="server.ssh_user">
          · user <span class="mono">{{ server.ssh_user }}</span>
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
      </div>
    </div>

    <div class="node-foot">
      <span class="tag">{{ containerLabel(server) }}</span>
      <NButton size="small" style="margin-left: auto" @click="emit('open')">
        Open node
      </NButton>
      <NButton size="small" @click="emit('edit')">
        Edit
      </NButton>
      <NButton
        size="small"
        :loading="validating"
        @click="emit('validate')"
      >
        Revalidate SSH
      </NButton>
      <NButton size="small" @click="emit('containers')">
        Containers
      </NButton>
      <NPopconfirm @positive-click="emit('delete')">
        <template #trigger>
          <NButton size="small" type="error" secondary>Delete</NButton>
        </template>
        Remove {{ server.name }}? Containers on the node are not touched.
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
