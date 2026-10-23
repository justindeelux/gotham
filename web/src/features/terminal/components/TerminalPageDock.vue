<script setup lang="ts">
import { ChevronDown, ChevronUp, ExternalLink, Play, X } from "@lucide/vue";
import { NButton } from "naive-ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import { useAuthStore } from "@/features/auth";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalTab } from "@/features/terminal/stores/terminal";
import { isContainerTerminalTarget } from "@/features/terminal/utils/terminal";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const authStore = useAuthStore();
const terminal = useTerminalStore();

const expanded = ref(true);
const narrow = ref(false);
let mediaQuery: MediaQueryList | null = null;

function syncViewport(matches: boolean): void {
  narrow.value = matches;
  expanded.value = !matches;
}

function onMediaChange(event: MediaQueryListEvent): void {
  syncViewport(event.matches);
}

const visibleTabs = computed<TerminalTab[]>(() =>
  terminal.pageTabs.filter((tab) =>
    isContainerTerminalTarget(tab.target) ? true : authStore.isPlatformAdmin,
  ),
);

const pausedTabs = computed<TerminalTab[]>(() =>
  visibleTabs.value.filter(
    (tab) => !terminal.isDisplayed(tab.id) && tab.sourcePath !== route.fullPath,
  ),
);

function target(tab: TerminalTab): string {
  if (isContainerTerminalTarget(tab.target)) {
    const label = tab.targetLabel || tab.containerName;
    return label || tab.applicationName || tab.title;
  }
  const where = tab.cwd && tab.cwd !== "." ? tab.cwd : tab.root;
  return where ? `${tab.serverName || tab.serverId} · ${where}` : tab.serverName || tab.serverId;
}

function resume(tab: TerminalTab): void {
  if (tab.sourcePath !== "") {
    void router.push(tab.sourcePath);
    return;
  }
  void router.push({ name: "terminal" });
}

function openInWindow(tab: TerminalTab): void {
  terminal.detachToWindow(tab.id);
}

async function close(tab: TerminalTab): Promise<void> {
  const closed = await terminal.requestClose([tab.id]);
  if (!closed) {
    return;
  }
  void nextTick(() => {
    const next =
      document.querySelector<HTMLElement>(".term-dock-resume") ??
      document.querySelector<HTMLElement>(".rail-btn--terminal");
    next?.focus();
  });
}

onMounted(() => {
  if (typeof window.matchMedia !== "function") {
    return;
  }
  mediaQuery = window.matchMedia("(max-width: 1024px)");
  syncViewport(mediaQuery.matches);
  mediaQuery.addEventListener("change", onMediaChange);
});

onBeforeUnmount(() => {
  mediaQuery?.removeEventListener("change", onMediaChange);
  mediaQuery = null;
});
</script>

<template>
  <section v-if="pausedTabs.length > 0" class="term-dock" :aria-label="t('terminal.dock.label')">
    <button
      type="button"
      class="term-dock-head"
      :aria-expanded="expanded"
      aria-controls="term-dock-list"
      :aria-label="expanded ? t('terminal.dock.collapse') : t('terminal.dock.expand')"
      @click="expanded = !expanded"
    >
      <span class="term-dock-title">{{ t("terminal.dock.heading") }}</span>
      <span class="term-dock-count mono">{{ pausedTabs.length }}</span>
      <component
        :is="expanded ? ChevronDown : ChevronUp"
        class="term-dock-chevron"
        :size="14"
        :stroke-width="1.8"
        aria-hidden="true"
      />
    </button>
    <ul v-show="expanded" id="term-dock-list" class="term-dock-list">
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
            :aria-label="t('terminal.dock.resumeTab', { title: tab.title })"
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
            :aria-label="t('terminal.dock.openInWindowTab', { title: tab.title })"
            :title="t('terminal.dock.openInWindowTab', { title: tab.title })"
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
  width: 100%;
  min-height: var(--space-6);
  padding: 0 var(--space-1);
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.term-dock-head:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

.term-dock-title {
  font-weight: 600;
  font-size: var(--text-sm);
  color: var(--fg-2);
}

.term-dock-count {
  margin-left: auto;
  min-width: var(--space-5);
  padding: 0 var(--space-1);
  border-radius: var(--radius-pill);
  background: var(--surface-warm);
  color: var(--muted);
  font-size: var(--text-xs);
  text-align: center;
}

.term-dock-chevron {
  flex: 0 0 auto;
  color: var(--muted);
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
  gap: calc(var(--space-1) / 4);
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
    bottom: var(--space-2);
    width: auto;
  }
}
</style>
