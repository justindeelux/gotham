<script setup lang="ts">
import { ExternalLink, Play, X } from "@lucide/vue";
import { NButton } from "naive-ui";
import { computed, nextTick } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalTab } from "@/features/terminal/stores/terminal";

/**
 * App-level host for page sessions (JUS-246).
 *
 * A page terminal outlives the route that opened it: the runtime keeps the
 * live PTY, and this dock renders a paused card for every page tab that no
 * mounted pane currently owns. That makes the session reachable from every
 * route (resume the source page, move it one-way into the window, or close it
 * through the shared confirmation) instead of leaving it alive but invisible.
 *
 * The dock never attaches the xterm host: the card is the paused state, not
 * a second view of the session, so the buffer keeps running in the runtime.
 */
const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const terminal = useTerminalStore();

/**
 * pausedTabs filters page tabs without a live pane. A tab whose source route
 * is already on screen is intentionally excluded: the page is mounting its
 * pane (or showing a connection/offline state), so a dock card would only
 * duplicate the same session.
 */
const pausedTabs = computed<TerminalTab[]>(() =>
  terminal.pageTabs.filter(
    (tab) => !terminal.isDisplayed(tab.id) && tab.sourcePath !== route.fullPath,
  ),
);

/** target describes the server and starting directory of one paused tab. */
function target(tab: TerminalTab): string {
  const where = tab.cwd && tab.cwd !== "." ? tab.cwd : tab.root;
  return where ? `${tab.serverName || tab.serverId} · ${where}` : tab.serverName || tab.serverId;
}

/** resume navigates back to the page/tab that owns the session. */
function resume(tab: TerminalTab): void {
  if (tab.sourcePath !== "") {
    void router.push(tab.sourcePath);
    return;
  }
  void router.push({ name: "terminal" });
}

/** openInWindow is the same one-way transfer the pane offers. */
function openInWindow(tab: TerminalTab): void {
  terminal.detachToWindow(tab.id);
}

/** close asks through the one shared close confirmation. */
async function close(tab: TerminalTab): Promise<void> {
  const closed = await terminal.requestClose([tab.id]);
  if (!closed) {
    return;
  }
  // The dock button may have just been removed; land focus on the next row or
  // the rail control instead of document.body.
  void nextTick(() => {
    const next =
      document.querySelector<HTMLElement>(".term-dock-resume") ??
      document.querySelector<HTMLElement>(".rail-btn--terminal");
    next?.focus();
  });
}
</script>

<template>
  <section
    v-if="pausedTabs.length > 0"
    class="term-dock"
    role="region"
    :aria-label="t('terminal.dock.label')"
  >
    <header class="term-dock-head">
      <span class="term-dock-title">{{ t("terminal.dock.heading") }}</span>
      <span class="term-dock-count mono">{{ pausedTabs.length }}</span>
    </header>
    <ul class="term-dock-list">
      <li v-for="tab in pausedTabs" :key="tab.id" class="term-dock-row">
        <span class="term-dock-status" :class="`is-${tab.status}`" aria-hidden="true" />
        <span class="term-dock-meta">
          <span class="term-dock-name" :title="tab.title">{{ tab.title }}</span>
          <span class="term-dock-target mono">{{ target(tab) }}</span>
          <span class="term-dock-state">
            {{ t("terminal.dock.paused") }} · {{ t(`terminal.status.${tab.status}`) }}
          </span>
        </span>
        <span class="term-dock-actions">
          <NButton
            size="tiny"
            secondary
            class="term-dock-resume"
            @click="resume(tab)"
          >
            <template #icon>
              <Play :size="13" :stroke-width="1.8" aria-hidden="true" />
            </template>
            {{ t("terminal.dock.resume") }}
          </NButton>
          <NButton
            size="tiny"
            quaternary
            :aria-label="t('terminal.dock.openInWindow')"
            :title="t('terminal.dock.openInWindow')"
            @click="openInWindow(tab)"
          >
            <template #icon>
              <ExternalLink :size="13" :stroke-width="1.8" aria-hidden="true" />
            </template>
          </NButton>
          <NButton
            size="tiny"
            quaternary
            class="term-dock-close"
            :aria-label="t('terminal.dock.closeTab', { title: tab.title })"
            @click="close(tab)"
          >
            <template #icon>
              <X :size="13" :stroke-width="1.8" aria-hidden="true" />
            </template>
          </NButton>
        </span>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.term-dock {
  position: fixed;
  left: calc(var(--rail-w) + var(--sidebar-w) + var(--space-4));
  bottom: var(--space-4);
  z-index: 66;
  width: min(
    24rem,
    calc(100vw - var(--rail-w) - var(--sidebar-w) - var(--space-8))
  );
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding: var(--space-2);
  background: var(--surface);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--elev-raised);
}

.term-dock-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  padding: 0 var(--space-1);
}

.term-dock-title {
  font-weight: 600;
  font-size: var(--text-sm);
  color: var(--fg-2);
}

.term-dock-count {
  min-width: var(--space-5);
  padding: 0 var(--space-1);
  border-radius: var(--radius-pill);
  background: var(--surface-warm);
  color: var(--muted);
  font-size: var(--text-xs);
  text-align: center;
}

.term-dock-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
  max-height: min(16rem, 40vh);
  overflow-y: auto;
}

.term-dock-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
  background: var(--surface-warm);
}

.term-dock-status {
  flex: 0 0 auto;
  width: var(--space-2);
  height: var(--space-2);
  border-radius: var(--radius-pill);
  background: var(--meta);
}

.term-dock-status.is-connected {
  background: var(--success);
}

.term-dock-status.is-connecting {
  background: var(--accent);
}

.term-dock-status.is-exited,
.term-dock-status.is-disconnected {
  background: var(--warn);
}

.term-dock-status.is-error {
  background: var(--danger);
}

.term-dock-meta {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
  flex: 1 1 auto;
}

.term-dock-name,
.term-dock-target {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.term-dock-name {
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.term-dock-target,
.term-dock-state {
  font-size: var(--text-xs);
  color: var(--meta);
}

.term-dock-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex: 0 0 auto;
}

.term-dock-close {
  min-width: var(--space-6);
  min-height: var(--space-6);
}

@media (max-width: 1024px) {
  .term-dock {
    left: var(--space-2);
    right: var(--space-2);
    width: auto;
  }
}
</style>
