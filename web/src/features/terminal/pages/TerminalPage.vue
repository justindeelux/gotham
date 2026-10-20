<script setup lang="ts">
import { NSelect } from "naive-ui";
import { useI18n } from "vue-i18n";

import TerminalServerPicker from "@/features/terminal/components/TerminalServerPicker.vue";
import TerminalSessionStage from "@/features/terminal/components/TerminalSessionStage.vue";
import { useTerminalPageSession } from "@/features/terminal/composables/useTerminalPageSession";

/**
 * Standalone page terminal (`/terminal`, JUS-246).
 *
 * This page is a thin composition: the server picker, the session stage and
 * `useTerminalPageSession` own the selection, confirmed switch, single-flight
 * start and explicit restart state. It never creates a shell on mount or on a
 * route/tab change; only a selection or an explicit button does.
 */
const { t } = useI18n();
const {
  servers,
  selectedId,
  selectedServer,
  serverReady,
  serverOptions,
  selectedStatusLabel,
  tabId,
  starting,
  closed,
  startSession,
  selectServer,
  onServerSelect,
} = useTerminalPageSession();
</script>

<template>
  <div class="term-page">
    <div class="page-head">
      <p class="eyebrow">{{ t("terminal.page.eyebrow") }}</p>
      <h1>{{ t("terminal.page.title") }}</h1>
      <p class="page-desc">{{ t("terminal.page.description") }}</p>
    </div>

    <div v-if="selectedId !== ''" class="term-toolbar">
      <label class="term-toolbar-label" for="term-server-select">
        {{ t("terminal.page.switchServer") }}
      </label>
      <NSelect
        id="term-server-select"
        class="term-server-select"
        size="small"
        :value="selectedId"
        :options="serverOptions"
        :aria-label="t('terminal.page.switchServer')"
        @update:value="onServerSelect"
      />
      <span
        v-if="selectedServer"
        class="term-toolbar-status"
        :class="`is-${selectedServer.status}`"
      >
        {{ selectedStatusLabel }}
      </span>
    </div>

    <TerminalServerPicker
      v-if="selectedId === ''"
      :servers="servers"
      @select="(serverId: string) => void selectServer(serverId)"
    />
    <TerminalSessionStage
      v-else
      :server="selectedServer"
      :ready="serverReady"
      :tab-id="tabId"
      :starting="starting"
      :closed="closed"
      @start="startSession"
    />
  </div>
</template>

<style scoped>
.term-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  height: 100%;
  min-height: 0;
}

.page-head {
  flex: 0 0 auto;
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
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

.term-toolbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.term-toolbar-label {
  font-size: var(--text-xs);
  color: var(--muted);
}

.term-server-select {
  max-width: 22rem;
}

.term-toolbar-status {
  font-size: var(--text-xs);
  color: var(--muted);
}

.term-toolbar-status.is-ready {
  color: var(--success);
}

.term-toolbar-status.is-error,
.term-toolbar-status.is-offline {
  color: var(--danger);
}

.term-toolbar-status.is-pending,
.term-toolbar-status.is-validating {
  color: var(--warn);
}

@media (max-width: 640px) {
  .term-toolbar {
    flex-wrap: wrap;
    row-gap: var(--space-1);
  }
}
</style>
