<script setup lang="ts">
import { ExternalLink, Keyboard, RefreshCw, X } from "@lucide/vue";
import { NButton, NPopover, NSpin } from "naive-ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalTab } from "@/features/terminal/stores/terminal";
import TerminalThemeMenu from "@/features/terminal/components/TerminalThemeMenu.vue";
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
const shortcutsOpen = ref<boolean>(false);
const hintId = computed<string>(() => `term-hint-${props.tabId}`);
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

/**
 * failureText is BOUNDED catalog copy only (JUS-248 r1). Backend response text
 * and relay phrases can carry host paths, so they are never rendered: every
 * known code maps to its translated message and anything else falls back to the
 * generic terminal copy.
 */
const failureText = computed<string>(() => {
  if (!tab.value) {
    return "";
  }
  return t(errorKey(tab.value.errorCode));
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

/**
 * onSurfaceClick focuses xterm only when the click is on the terminal itself.
 * Clicks on the meta bar (theme menu, shortcuts, page actions) and on any
 * interactive control must keep their own focus: a button activated with
 * Enter must never hand the next keystroke to the shell.
 */
function onSurfaceClick(event: MouseEvent): void {
  const target = event.target;
  if (
    target instanceof Element &&
    target.closest(
      "button, a, input, label, [role='radio'], [role='radiogroup'], .term-meta, .n-popover",
    )
  ) {
    return;
  }
  focusTerminal();
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

/**
 * onSurfaceKeydown provides the documented escape from xterm's input trap.
 * Ctrl+Shift+E moves focus out of the shell to the window tab (or the page
 * pane's action), so keyboard users are never stuck in the terminal.
 *
 * The chord deliberately avoids Ctrl+Shift+Escape: Windows reserves it for
 * Task Manager, so the browser never sees it. Shift+Tab also leaves the
 * shell and is documented in the shortcuts popover.
 */
function onSurfaceKeydown(event: KeyboardEvent): void {
  if (!event.ctrlKey || !event.shiftKey || event.code !== "KeyE") {
    return;
  }
  event.preventDefault();
  event.stopPropagation();
  const element = surface.value;
  if (!element) {
    return;
  }
  const windowRoot = element.closest(".term-window");
  if (windowRoot) {
    document.getElementById(`term-tab-${props.tabId}`)?.focus();
    return;
  }
  const action = element.parentElement?.querySelector<HTMLElement>(
    ".term-open-in-window",
  );
  action?.focus();
}

onMounted(attachSurface);
onBeforeUnmount(() => {
  const element = surface.value;
  if (element) {
    terminal.detach(props.tabId, element);
  }
});
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
    :aria-describedby="hintId"
    aria-keyshortcuts="Control+Shift+E"
    :data-theme="terminal.theme"
    @click="onSurfaceClick"
    @keydown.capture="onSurfaceKeydown"
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
      <NPopover
        v-model:show="shortcutsOpen"
        trigger="click"
        placement="bottom-end"
      >
        <template #trigger>
          <NButton
            quaternary
            size="tiny"
            class="term-shortcuts-btn"
            :aria-label="t('terminal.shortcuts.show')"
          >
            <template #icon>
              <Keyboard :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
          </NButton>
        </template>
        <div class="term-shortcuts">
          <p class="term-shortcuts-title">{{ t("terminal.shortcuts.title") }}</p>
          <dl>
            <div>
              <dt class="mono">Alt+Shift+W</dt>
              <dd>{{ t("terminal.shortcuts.closeWindow") }}</dd>
            </div>
            <div>
              <dt class="mono">Alt+M</dt>
              <dd>{{ t("terminal.shortcuts.minimize") }}</dd>
            </div>
            <div>
              <dt class="mono">Delete</dt>
              <dd>{{ t("terminal.shortcuts.closeTab") }}</dd>
            </div>
            <div>
              <dt class="mono">Ctrl+Shift+E</dt>
              <dd>{{ t("terminal.shortcuts.escape") }}</dd>
            </div>
          </dl>
          <p class="term-shortcuts-note small muted">
            {{ t("terminal.shortcuts.hint") }}
          </p>
        </div>
      </NPopover>
      <span class="grow" />
      <TerminalThemeMenu />
      <template v-if="variant === 'page'">
        <NButton
          size="tiny"
          secondary
          class="term-open-in-window"
          @click.stop="detachToWindow"
        >
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

    <p :id="hintId" class="term-sr">
      {{ t("terminal.shortcuts.hint") }}
    </p>
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

/* The light renderer gets a matching frame instead of a dark gutter around
 * the xterm canvas. The meta/status bars stay app chrome. */
.term-surface[data-theme="gotham-light"] {
  background: var(--term-light-bg);
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
  font-size: var(--text-xs);
}

.term-session {
  color: var(--meta);
  font-size: var(--text-xs);
}

.term-shortcuts-btn {
  /* WCAG 2.5.8: the trigger keeps a 24x24 target (Naive's tiny 26x22 is
   * below the minimum). The visual icon stays small; the hit box grows. */
  min-width: var(--space-6);
  min-height: var(--space-6);
  margin-left: var(--space-1);
}

.term-shortcuts-note {
  max-width: 26rem;
}

.term-shortcuts {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  max-width: min(26rem, 80vw);
}

.term-shortcuts-title {
  margin: 0;
  font-weight: 600;
  color: var(--fg-2);
}

.term-shortcuts dl {
  display: grid;
  gap: var(--space-1);
  margin: 0;
}

.term-shortcuts dl > div {
  display: grid;
  grid-template-columns: minmax(0, 9rem) minmax(0, 1fr);
  gap: var(--space-2);
  align-items: baseline;
}

.term-shortcuts dt {
  margin: 0;
  color: var(--fg-2);
  font-size: var(--text-xs);
}

.term-shortcuts dd {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-xs);
}

.term-sr {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
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
  max-width: min(26rem, 100%);
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
  width: var(--space-2);
  height: var(--space-2);
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

/* Phone width (390px): the meta row wraps instead of clipping the working
 * directory to a single character. The key labels and the session id are
 * diagnostic only; the cwd takes its own full row, while the action buttons
 * stay on the first row. */
@media (max-width: 520px) {
  .term-meta {
    flex-wrap: wrap;
    row-gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
  }

  .term-key,
  .term-session {
    display: none;
  }

  .term-cwd {
    order: 10;
    flex: 1 1 100%;
  }

  .term-shortcuts-btn {
    margin-left: 0;
  }
}
</style>
