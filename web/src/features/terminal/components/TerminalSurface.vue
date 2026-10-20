<script setup lang="ts">
import { ExternalLink, RefreshCw, X } from "@lucide/vue";
import { NButton, NSpin } from "naive-ui";
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalTab } from "@/features/terminal/stores/terminal";
import {
  errorKey,
  isLive,
  isRetryable,
  shortSessionId,
  statusTone,
} from "@/features/terminal/utils/terminal";

/**
 * Shared terminal surface (JUS-245): the one place xterm is displayed. Page
 * mode and the floating window both mount this component; it merely offers
 * its element to the tab's runtime, which owns the renderer, so moving between
 * the two re-parents the live buffer instead of reconnecting.
 */
interface Props {
  tabId: string;
  variant: "page" | "window";
  /** labelledBy names the window tab this panel belongs to. */
  labelledBy?: string;
}

const props = withDefaults(defineProps<Props>(), { labelledBy: "" });
const { t } = useI18n();
const terminal = useTerminalStore();
const surface = ref<HTMLElement | null>(null);
const tab = computed<TerminalTab | null>(() => terminal.findTab(props.tabId));

/** statusText is the localized lifecycle label. */
const statusText = computed<string>(() => {
  const status = tab.value?.status ?? "connecting";
  return t(`terminal.status.${status}`);
});

const tone = computed<string>(() => statusTone(tab.value?.status ?? "connecting"));
const retryable = computed<boolean>(() =>
  isRetryable(tab.value?.status ?? "connecting"),
);

/** failureText prefers the control-plane message for an unknown failure. */
const failureText = computed<string>(() => {
  const current = tab.value;
  if (!current) {
    return "";
  }
  if (current.errorCode !== null) {
    return t(errorKey(current.errorCode));
  }
  return current.errorMessage || t("terminal.errors.generic");
});

/** exitText describes how the shell ended. */
const exitText = computed<string>(() => {
  const current = tab.value;
  if (!current) {
    return "";
  }
  if (current.exitSignal) {
    return t("terminal.exited.withSignal", { signal: current.exitSignal });
  }
  if (current.exitCode !== null) {
    return t("terminal.exited.withCode", { code: current.exitCode });
  }
  return t("terminal.exited.ended");
});

/** surfaceLabel names the region for screen readers. */
const surfaceLabel = computed<string>(() =>
  t("terminal.pane.detailLabel", {
    title: tab.value?.title ?? t("terminal.window.heading"),
  }),
);

/** attachSurface hands the display element to the tab runtime. */
function attachSurface(): void {
  const element = surface.value;
  if (element) {
    terminal.attach(props.tabId, element);
  }
}

/** focusTerminal keeps a click on the pane landing in the shell. */
function focusTerminal(): void {
  terminal.focus(props.tabId);
}

/** detachToWindow is the one-way page → window transfer. */
function detachToWindow(): void {
  terminal.detachToWindow(props.tabId);
}

/** closeTerminal uses the same confirmation path as every close control. */
function closeTerminal(): void {
  terminal.requestClose([props.tabId]);
}

/** retrySession starts a new PTY; it never replays prior input. */
function retrySession(): void {
  void terminal.retry(props.tabId);
}

onMounted(attachSurface);
watch(() => props.tabId, attachSurface);
// A retry swaps the runtime under the still-mounted surface; re-attach once
// the new session exists so the fresh renderer lands in the same pane.
watch(
  () => tab.value?.sessionId,
  () => {
    if (tab.value?.sessionId) {
      void nextTick(attachSurface);
    }
  },
);
</script>

<template>
  <section
    v-if="tab"
    class="term-surface"
    :class="`term-surface--${variant}`"
    :role="variant === 'window' ? 'tabpanel' : 'region'"
    :aria-label="variant === 'window' ? undefined : surfaceLabel"
    :aria-labelledby="variant === 'window' ? labelledBy : undefined"
    @click="focusTerminal"
  >
    <div class="term-meta">
      <span class="term-chip">
        <span class="term-key">{{ t("terminal.pane.target") }}</span>
        <span class="mono">{{ tab.target }}</span>
      </span>
      <span v-if="tab.cwd" class="term-chip term-cwd" :title="tab.cwd">
        <span class="term-key">{{ t("terminal.pane.cwd") }}</span>
        <span class="mono">{{ tab.cwd }}</span>
      </span>
      <span
        v-if="tab.sessionId"
        class="term-session mono"
        :title="tab.sessionId"
      >
        {{ shortSessionId(tab.sessionId) }}
      </span>
      <span class="grow" />
      <template v-if="variant === 'page'">
        <NButton size="tiny" secondary @click.stop="detachToWindow">
          <template #icon>
            <ExternalLink :size="13" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("terminal.pane.openInWindow") }}
        </NButton>
        <NButton
          quaternary
          size="tiny"
          :aria-label="t('terminal.pane.close')"
          @click.stop="closeTerminal"
        >
          <template #icon>
            <X :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
        </NButton>
      </template>
    </div>

    <div ref="surface" class="term-screen" data-testid="terminal-screen" />

    <div
      v-if="tab.status !== 'connected'"
      class="term-overlay"
      :class="`term-overlay--${tone}`"
    >
      <div v-if="tab.status === 'connecting'" class="term-overlay-body">
        <NSpin size="small" :aria-label="t('terminal.status.connecting')" />
        <p class="small muted">{{ t("terminal.status.connecting") }}</p>
      </div>
      <div v-else-if="tab.status === 'exited'" class="term-overlay-body">
        <p class="term-overlay-title">{{ t("terminal.exited.title") }}</p>
        <p class="small muted">{{ exitText }}</p>
        <NButton size="small" secondary @click.stop="retrySession">
          <template #icon>
            <RefreshCw :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("terminal.actions.retry") }}
        </NButton>
      </div>
      <div v-else-if="tab.status === 'disconnected'" class="term-overlay-body">
        <p class="term-overlay-title">{{ t("terminal.disconnected.title") }}</p>
        <p class="small muted">{{ t("terminal.disconnected.body") }}</p>
        <NButton size="small" secondary @click.stop="retrySession">
          <template #icon>
            <RefreshCw :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("terminal.actions.retry") }}
        </NButton>
      </div>
      <div v-else-if="tab.status === 'error'" class="term-overlay-body">
        <p class="term-overlay-title">{{ t("terminal.errors.title") }}</p>
        <p class="small muted">{{ failureText }}</p>
        <p v-if="tab.errorMessage && tab.errorCode !== null" class="small term-detail mono">
          {{ tab.errorMessage }}
        </p>
        <NButton v-if="retryable" size="small" secondary @click.stop="retrySession">
          <template #icon>
            <RefreshCw :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("terminal.actions.retry") }}
        </NButton>
      </div>
    </div>

    <footer class="term-statusbar">
      <span class="term-state" :class="`term-state--${tone}`" role="status" aria-live="polite">
        {{ statusText }}
      </span>
      <span v-if="tab.shell" class="term-status-item mono">
        {{ tab.shell }}
      </span>
      <span class="term-status-item mono">
        {{
          t("terminal.pane.size", {
            cols: tab.cols,
            rows: tab.rows,
          })
        }}
      </span>
      <span class="grow" />
      <span
        v-if="isLive(tab.status) && tab.serviceUser"
        class="term-status-item term-user"
        :title="t('terminal.pane.privilege')"
      >
        {{ t("terminal.pane.serviceUser", { user: tab.serviceUser }) }}
        <template v-if="variant === 'page'">
          · {{ t("terminal.pane.privilege") }}
        </template>
      </span>
    </footer>
  </section>
</template>

<style scoped>
.term-surface {
  position: relative;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  height: 100%;
  background: var(--surface-warm);
}

.term-meta {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}

.term-chip {
  display: inline-flex;
  align-items: baseline;
  gap: var(--space-1);
  min-width: 0;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.term-cwd {
  overflow: hidden;
}

.term-cwd .mono {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.term-key {
  color: var(--meta);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  font-size: 10px;
}

.term-session {
  color: var(--meta);
  font-size: 10px;
}

.term-screen {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
  padding: var(--space-2);
  overflow: hidden;
}

/* The runtime appends its xterm host here; the host owns the full box so
 * FitAddon measures the same rounded number of rows. */
.term-screen :deep(.term-host) {
  width: 100%;
  height: 100%;
}

.term-overlay {
  position: absolute;
  inset: auto var(--space-3) calc(var(--space-8) + var(--space-2));
  display: grid;
  place-items: center;
  pointer-events: none;
}

.term-overlay-body {
  pointer-events: auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  max-width: 420px;
  padding: var(--space-4);
  text-align: center;
  background: color-mix(in oklab, var(--surface) 92%, transparent);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--elev-raised);
}

.term-overlay-title {
  margin: 0;
  font-weight: 600;
  color: var(--fg-2);
}

.term-detail {
  max-width: 100%;
  overflow-wrap: anywhere;
  color: var(--meta);
}

.term-statusbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-1) var(--space-3);
  border-top: 1px solid var(--border);
  background: var(--surface);
  font-size: var(--text-xs);
  color: var(--muted);
  min-width: 0;
}

.term-state {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  white-space: nowrap;
}

.term-state::before {
  content: "";
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
  background: currentColor;
}

.term-state--success {
  color: var(--success);
}

.term-state--accent {
  color: var(--accent);
}

.term-state--warn {
  color: var(--warn);
}

.term-state--danger {
  color: var(--danger);
}

.term-state--muted {
  color: var(--meta);
}

.term-status-item {
  white-space: nowrap;
}

.term-user {
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--meta);
}
</style>
