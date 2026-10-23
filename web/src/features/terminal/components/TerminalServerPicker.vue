<script setup lang="ts">
import { NButton, NCard, NEmpty } from "naive-ui";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";

import type { Server } from "@/features/servers";
import { serverStatusKey } from "@/features/servers";

interface Props {
  servers: Server[];
}

defineProps<Props>();
const emit = defineEmits<{ select: [serverId: string] }>();
const { t } = useI18n();

function statusLabel(server: Server): string {
  return t(serverStatusKey(server.status));
}
</script>

<template>
  <div class="term-chooser">
    <NCard>
      <div v-if="servers.length > 0" class="server-picker">
        <button
          v-for="server in servers"
          :key="server.id"
          type="button"
          class="server-pick"
          :class="{ 'is-unavailable': server.status !== 'ready' }"
          @click="emit('select', server.id)"
        >
          <span class="term-pick-head">
            <span class="term-pick-name">{{ server.name }}</span>
            <span class="term-pick-status" :class="`is-${server.status}`">
              {{ statusLabel(server) }}
            </span>
          </span>
          <span class="term-pick-meta mono">{{ server.ip }}</span>
          <span class="term-pick-go">{{ t("terminal.page.chooseServer") }}</span>
        </button>
      </div>
      <NEmpty v-else :description="t('terminal.page.empty')">
        <template #extra>
          <p class="term-empty-hint">{{ t("terminal.page.emptyHint") }}</p>
          <RouterLink :to="{ name: 'servers' }" custom>
            <template #default="{ navigate }">
              <NButton type="primary" @click="navigate">
                {{ t("terminal.page.addServer") }}
              </NButton>
            </template>
          </RouterLink>
        </template>
      </NEmpty>
    </NCard>
  </div>
</template>

<style scoped>
.term-chooser {
  flex: 0 0 auto;
}

.server-picker {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr));
  gap: var(--space-3);
}

.server-pick {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition:
    border-color var(--motion-base) var(--ease-standard),
    background var(--motion-base) var(--ease-standard);
}

.server-pick:hover {
  border-color: var(--accent);
  background: color-mix(in oklab, var(--surface) 88%, var(--accent));
}

.server-pick:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

.server-pick.is-unavailable {
  opacity: 0.72;
}

.term-pick-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}

.term-pick-name {
  font-weight: 600;
  color: var(--fg-2);
}

.term-pick-status {
  font-size: var(--text-xs);
  color: var(--muted);
}

.term-pick-status.is-ready {
  color: var(--success);
}

.term-pick-status.is-error,
.term-pick-status.is-offline {
  color: var(--danger);
}

.term-pick-status.is-pending,
.term-pick-status.is-validating {
  color: var(--warn);
}

.term-pick-meta {
  color: var(--muted);
  font-size: var(--text-xs);
}

.term-pick-go {
  margin-top: var(--space-2);
  color: var(--accent-ink);
  font-size: var(--text-xs);
}

.term-empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
}
</style>
