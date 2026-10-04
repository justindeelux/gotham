<script setup lang="ts">
import { NButton, NSelect, NTooltip } from "naive-ui";
import { toRef } from "vue";

import { useServiceLogs } from "@/features/services/composables/useServiceLogs";

interface Props {
  serviceId: string;
  /** Compose service names the project declares; empty means all services. */
  services?: string[];
  title?: string;
  maxLines?: number;
}

/**
 * Live log terminal for one compose service (see `useServiceLogs` for the
 * stream lifecycle).
 *
 * Known gap, rendered as an explicit stub below: the API has no stored log
 * artifact, so "Download" cannot export anything that is not already on
 * screen.
 */
const props = withDefaults(defineProps<Props>(), {
  services: () => [],
  title: "Service logs",
  maxLines: 2000,
});

const {
  status,
  error,
  lines,
  isPaused,
  isFollowing,
  selectedService,
  logBody,
  serviceOptions,
  statusLabel,
  statusClasses,
  start,
  stop,
  togglePause,
  toggleFollow,
  clearLines,
  handleScroll,
} = useServiceLogs({
  serviceId: toRef(props, "serviceId"),
  services: toRef(props, "services"),
  maxLines: toRef(props, "maxLines"),
});
</script>

<template>
  <section class="service-logs">
    <div class="service-logs__toolbar">
      <NSelect
        v-model:value="selectedService"
        :options="serviceOptions"
        size="small"
        style="width: 200px"
        aria-label="Compose service to stream"
      />
      <NButton
        v-if="status === 'streaming' || status === 'connecting'"
        size="small"
        type="error"
        secondary
        @click="stop"
      >
        Stop
      </NButton>
      <NButton v-else size="small" type="primary" secondary @click="start">
        Stream logs
      </NButton>
      <NButton
        size="small"
        secondary
        :disabled="status !== 'streaming'"
        @click="togglePause"
      >
        {{ isPaused ? "Resume" : "Pause" }}
      </NButton>
      <NButton
        size="small"
        secondary
        :type="isFollowing ? 'primary' : 'default'"
        @click="toggleFollow"
      >
        {{ isFollowing ? "Following" : "Follow" }}
      </NButton>
      <NButton size="small" secondary @click="clearLines">Clear</NButton>
      <NTooltip trigger="hover">
        <template #trigger>
          <NButton size="small" secondary disabled>Download</NButton>
        </template>
        Backend pending: the API serves a live stream only, so there is no
        stored log file to download.
      </NTooltip>
      <span class="realtime" :class="statusClasses">{{ statusLabel }}</span>
      <span class="service-logs__count">{{ lines.length }} lines</span>
    </div>

    <p v-if="error" class="service-logs__error">{{ error }}</p>

    <div ref="logBody" class="log mono" @scroll="handleScroll">
      <p v-if="lines.length === 0" class="service-logs__empty">
        {{
          status === "idle"
            ? "No stream open. Select a compose service and start streaming."
            : "Waiting for log output…"
        }}
      </p>
      <div v-for="line in lines" :key="line.id" class="log-line">
        {{ line.text }}
      </div>
    </div>

    <p class="service-logs__channel">
      Channel: <span class="mono">GET /api/v1/services/{{
        serviceId.slice(0, 8)
      }}…/logs?follow=true</span> · agent runs
      <span class="mono">docker compose logs -f</span>.
    </p>
  </section>
</template>

<style scoped>
.service-logs {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.service-logs__toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.service-logs__count {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
}

.service-logs__error {
  margin: 0;
  font-size: var(--text-sm);
  color: var(--danger-ink);
}

.realtime {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--success-ink);
  background: var(--success-soft);
  border-radius: var(--radius-pill);
  padding: 2px 8px;
  white-space: nowrap;
}

.realtime.is-paused {
  color: var(--warn-ink);
  background: var(--warn-soft);
}

.realtime.is-offline {
  color: var(--muted);
  background: var(--surface-warm);
}

.realtime.is-error {
  color: var(--danger-ink);
  background: var(--danger-soft);
}

.log {
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  font-size: var(--text-xs);
  line-height: 1.55;
  padding: var(--space-3);
  overflow: auto;
  min-height: 240px;
  max-height: 52vh;
}

.log-line {
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--fg);
}

.service-logs__empty {
  margin: 0;
  color: var(--muted);
}

.service-logs__channel {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}
</style>
