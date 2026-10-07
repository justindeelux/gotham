<script setup lang="ts">
import { NButton, NSelect, NTooltip } from "naive-ui";
import { computed, toRef } from "vue";

import { useServiceLogs } from "@/features/services/composables/useServiceLogs";
import { activeLocale, i18n } from "@/shared/i18n";

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
  title: "",
  maxLines: 2000,
});

/**
 * t renders log-terminal copy in the active locale (tracks language
 * switches). Called during render, so controls refresh without dropping
 * the stream. Streamed log lines stay raw and are never translated.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

/** heading is the caller override or the localized logs wording. */
const heading = computed<string>(() => props.title || t("services.logs.title"));

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
  <section class="service-logs" :aria-label="heading">
    <div class="service-logs__toolbar">
      <NSelect
        v-model:value="selectedService"
        :options="serviceOptions"
        size="small"
        style="width: 200px"
        :aria-label="t('services.logs.serviceAria')"
      />
      <NButton
        v-if="status === 'streaming' || status === 'connecting'"
        size="small"
        type="error"
        secondary
        @click="stop"
      >
        {{ t("services.logs.stop") }}
      </NButton>
      <NButton v-else size="small" type="primary" secondary @click="start">
        {{ t("services.logs.stream") }}
      </NButton>
      <NButton
        size="small"
        secondary
        :disabled="status !== 'streaming'"
        @click="togglePause"
      >
        {{ isPaused ? t("services.logs.resume") : t("services.logs.pause") }}
      </NButton>
      <NButton
        size="small"
        secondary
        :type="isFollowing ? 'primary' : 'default'"
        @click="toggleFollow"
      >
        {{ isFollowing ? t("services.logs.following") : t("services.logs.follow") }}
      </NButton>
      <NButton size="small" secondary @click="clearLines">{{ t("services.logs.clear") }}</NButton>
      <NTooltip trigger="hover">
        <template #trigger>
          <NButton size="small" secondary disabled>{{ t("services.logs.download") }}</NButton>
        </template>
        {{ t("services.logs.downloadTip") }}
      </NTooltip>
      <span class="realtime" :class="statusClasses">{{ statusLabel }}</span>
      <span class="service-logs__count">{{
          t(lines.length === 1 ? "services.logs.linesOne" : "services.logs.linesOther", {
            count: lines.length,
          })
        }}</span>
    </div>

    <p v-if="error" class="service-logs__error">{{ error }}</p>

    <div ref="logBody" class="log mono" @scroll="handleScroll">
      <p v-if="lines.length === 0" class="service-logs__empty">
        {{
          status === "idle"
            ? t("services.logs.emptyIdle")
            : t("services.logs.emptyWaiting")
        }}
      </p>
      <div v-for="line in lines" :key="line.id" class="log-line">
        {{ line.text }}
      </div>
    </div>

    <p class="service-logs__channel">
      {{
        t("services.logs.channel", {
          url: `GET /api/v1/services/${serviceId.slice(0, 8)}…/logs?follow=true`,
          cmd: "docker compose logs -f",
        })
      }}
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
