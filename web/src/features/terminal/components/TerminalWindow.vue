<script setup lang="ts">
import { X } from "@lucide/vue";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import type { CSSProperties } from "vue";
import { useI18n } from "vue-i18n";

import TerminalCloseDialog from "@/features/terminal/components/TerminalCloseDialog.vue";
import TerminalSurface from "@/features/terminal/components/TerminalSurface.vue";
import TerminalTargetPicker from "@/features/terminal/components/TerminalTargetPicker.vue";
import {
  TERMINAL_WINDOW_CHROME_MARGIN,
  TERMINAL_WINDOW_MIN_HEIGHT,
  TERMINAL_WINDOW_MIN_WIDTH,
  TERMINAL_WINDOW_SIDE_MARGIN,
} from "@/features/terminal/schemas/terminal";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import { useTerminalWindowStore } from "@/features/terminal/stores/terminalWindow";
import FloatingWindow from "@/shared/ui/FloatingWindow.vue";
import type { WindowBox } from "@/shared/window/geometry";
import { windowResizeLimits } from "@/shared/window/geometry";

const { t } = useI18n();
const terminal = useTerminalStore();
const windowStore = useTerminalWindowStore();

const tabsElement = ref<HTMLElement | null>(null);
const viewport = ref({
  width: typeof window === "undefined" ? 0 : window.innerWidth,
  height: typeof window === "undefined" ? 0 : window.innerHeight,
});

const tabs = computed(() => terminal.windowTabs);
const activeTab = computed(() => {
  const id = terminal.activeTabId;
  return tabs.value.find((tab) => tab.id === id) ?? tabs.value[0] ?? null;
});

const displayedSize = computed<{ width: number; height: number }>(() => {
  const fitted = windowStore.displayGeometry(
    viewport.value.width,
    viewport.value.height,
  );
  return { width: fitted.width, height: fitted.height };
});

const resizeLimits = computed(() =>
  windowResizeLimits(
    viewport.value.width,
    viewport.value.height,
    {
      side: TERMINAL_WINDOW_SIDE_MARGIN,
      chrome: TERMINAL_WINDOW_CHROME_MARGIN,
    },
    TERMINAL_WINDOW_MIN_WIDTH,
    TERMINAL_WINDOW_MIN_HEIGHT,
  ),
);

const windowStyle = computed<CSSProperties>(() => {
  const fitted = windowStore.displayGeometry(
    viewport.value.width,
    viewport.value.height,
  );
  const style: Record<string, string> = {
    width: `${displayedSize.value.width}px`,
    height: `${displayedSize.value.height}px`,
    "--term-window-chrome": `${TERMINAL_WINDOW_CHROME_MARGIN}px`,
    "--term-window-z": windowStore.raised ? "72" : "68",
  };
  if (fitted.x !== null && fitted.y !== null) {
    style.left = `${fitted.x}px`;
    style.top = `${fitted.y}px`;
    style.right = "auto";
    style.bottom = "auto";
  }
  return style as CSSProperties;
});

function onWindowResize(): void {
  viewport.value = { width: window.innerWidth, height: window.innerHeight };
}

function focusWindow(): void {
  windowStore.raise();
}

function onDocumentPointerDown(event: PointerEvent): void {
  if (
    !windowStore.raised ||
    (event.target instanceof Element && event.target.closest(".term-window"))
  ) {
    return;
  }
  windowStore.lower();
}

function onWindowDrag(x: number, y: number): void {
  windowStore.setPosition(
    x,
    y,
    displayedSize.value.width,
    displayedSize.value.height,
  );
}

function onWindowResizeMove(box: WindowBox): void {
  windowStore.resizeTo(box, viewport.value.width, viewport.value.height);
}

function onWindowResizeBy(deltaWidth: number, deltaHeight: number): void {
  windowStore.resizeBy(
    deltaWidth,
    deltaHeight,
    viewport.value.width,
    viewport.value.height,
  );
}

function minimizeWindow(): void {
  windowStore.setMinimized(true);
  void nextTick(() => {
    document
      .querySelector<HTMLElement>(".rail-btn--terminal")
      ?.focus();
  });
}

function restoreWindow(): void {
  windowStore.setMinimized(false);
}

function focusRestoredWindow(): void {
  const tab = activeTab.value;
  if (!tab) {
    return;
  }
  terminal.fit(tab.id);
  document.getElementById(`term-tab-${tab.id}`)?.focus();
}

watch(
  () => windowStore.minimized,
  (next, previous) => {
    if (previous === true && next === false) {
      void nextTick(focusRestoredWindow);
    }
  },
);

watch(
  () => activeTab.value?.id,
  () => {
    const tab = activeTab.value;
    if (!tab) {
      return;
    }
    void nextTick(() => {
      terminal.fit(tab.id);
      revealActiveTab();
    });
  },
);

function requestCloseTab(id: string): void {
  terminal.requestClose([id]);
}

function requestCloseWindow(): void {
  terminal.requestClose(tabs.value.map((tab) => tab.id));
}

function activateTab(id: string): void {
  terminal.activate(id);
}

function focusTab(index: number): void {
  const tab = tabs.value[index];
  if (!tab) {
    return;
  }
  terminal.activate(tab.id);
  void nextTick(() => {
    document.getElementById(`term-tab-${tab.id}`)?.focus();
  });
}

function revealActiveTab(): void {
  const strip = tabsElement.value;
  const active = activeTab.value;
  if (!strip || !active) {
    return;
  }
  const element = document.getElementById(`term-tab-${active.id}`);
  if (!element) {
    return;
  }
  const stripRect = strip.getBoundingClientRect();
  const tabRect = element.getBoundingClientRect();
  if (tabRect.left < stripRect.left) {
    strip.scrollLeft -= stripRect.left - tabRect.left;
  } else if (tabRect.right > stripRect.right) {
    strip.scrollLeft += tabRect.right - stripRect.right;
  }
}

function onTabKeydown(event: KeyboardEvent, index: number): void {
  const count = tabs.value.length;
  if (count === 0) {
    return;
  }
  if (event.key === "ArrowRight") {
    event.preventDefault();
    focusTab((index + 1) % count);
  } else if (event.key === "ArrowLeft") {
    event.preventDefault();
    focusTab((index - 1 + count) % count);
  } else if (event.key === "Home") {
    event.preventDefault();
    focusTab(0);
  } else if (event.key === "End") {
    event.preventDefault();
    focusTab(count - 1);
  } else if (event.key === "Delete") {
    event.preventDefault();
    requestCloseTab(tabs.value[index]!.id);
  }
}

function onGlobalKeydown(event: KeyboardEvent): void {
  if (
    event.defaultPrevented ||
    event.isComposing ||
    windowStore.minimized ||
    tabs.value.length === 0 ||
    terminal.closeRequest !== null
  ) {
    return;
  }
  if (event.target instanceof Element && event.target.closest(".n-modal")) {
    return;
  }
  const active = document.activeElement;
  if (active instanceof Element && active.closest(".xterm") !== null) {
    return;
  }
  const inWindow =
    active instanceof Element && active.closest(".term-window") !== null;
  const inTerminal =
    active instanceof Element && active.closest(".term-surface") !== null;
  if (event.altKey && event.shiftKey && event.code === "KeyW") {
    if (!inWindow && !inTerminal) {
      return;
    }
    event.preventDefault();
    requestCloseWindow();
    return;
  }
  if (event.altKey && !event.shiftKey && event.code === "KeyM") {
    if (!inWindow) {
      return;
    }
    event.preventDefault();
    minimizeWindow();
  }
}

function onConfirmed(): void {
  void nextTick(() => {
    const active = activeTab.value;
    if (active) {
      document.getElementById(`term-tab-${active.id}`)?.focus();
      return;
    }
    const pageTab = terminal.pageTabs[0];
    if (pageTab) {
      terminal.focus(pageTab.id);
      return;
    }
    const fallback =
      document.querySelector<HTMLElement>(".term-dock-resume") ??
      document.querySelector<HTMLElement>(".rail-btn--terminal") ??
      document.querySelector<HTMLElement>(".rail .rail-btn");
    fallback?.focus();
  });
}

function onBeforeUnload(event: BeforeUnloadEvent): void {
  if (!terminal.hasLiveSessions) {
    return;
  }
  event.preventDefault();
  event.returnValue = "";
}

watch(
  () => tabs.value.length,
  (count, previous) => {
    if (count > (previous ?? 0)) {
      windowStore.raise();
    }
    if (count > (previous ?? 0) && (previous === 0 || windowStore.minimized)) {
      windowStore.setMinimized(false);
    }
  },
);

onMounted(() => {
  window.addEventListener("resize", onWindowResize);
  window.addEventListener("keydown", onGlobalKeydown, true);
  window.addEventListener("pointerdown", onDocumentPointerDown, true);
  window.addEventListener("beforeunload", onBeforeUnload);
  onWindowResize();
});

onBeforeUnmount(() => {
  window.removeEventListener("resize", onWindowResize);
  window.removeEventListener("keydown", onGlobalKeydown, true);
  window.removeEventListener("pointerdown", onDocumentPointerDown, true);
  window.removeEventListener("beforeunload", onBeforeUnload);
});

defineExpose({ restoreWindow });
</script>

<template>
  <FloatingWindow
    v-if="tabs.length > 0"
    v-show="!windowStore.minimized"
    class="term-window"
    :style="windowStyle"
    :window-label="t('terminal.window.label')"
    :minimize-label="t('terminal.window.minimize')"
    :close-label="t('terminal.window.close')"
    :resize-label="t('terminal.window.resize')"
    :resize-value-text="
      t('terminal.window.resizeValue', {
        width: displayedSize.width,
        height: displayedSize.height,
      })
    "
    :size="displayedSize"
    :resize-limits="resizeLimits"
    :class-names="{
      bar: 'term-window-bar',
      min: 'term-window-min',
      close: 'term-window-close',
      resize: 'term-resize',
    }"
    @minimize="minimizeWindow"
    @close="requestCloseWindow"
    @drag="onWindowDrag"
    @resize="onWindowResizeMove"
    @resize-by="onWindowResizeBy"
    @pointerdown="focusWindow"
    @focusin="focusWindow"
  >
    <template #title>{{ t("terminal.window.heading") }}</template>
    <template #context>
      <span v-if="activeTab" class="term-window-context mono" :title="activeTab.title">
        {{ activeTab.title }}
      </span>
    </template>

    <template #tabs>
      <div class="term-tabstrip">
        <div
          ref="tabsElement"
          class="term-tabs"
          role="tablist"
          :aria-label="t('terminal.window.tabs')"
        >
          <div
            v-for="(tab, index) in tabs"
            :key="tab.id"
            class="term-tab"
            :class="{ 'is-active': tab.id === activeTab?.id }"
            role="presentation"
          >
            <button
              :id="`term-tab-${tab.id}`"
              type="button"
              class="term-tab-name"
              role="tab"
              :aria-selected="tab.id === activeTab?.id"
              :aria-controls="`term-panel-${tab.id}`"
              :tabindex="tab.id === activeTab?.id ? 0 : -1"
              :title="tab.title"
              aria-keyshortcuts="Delete"
              @click="activateTab(tab.id)"
              @keydown="onTabKeydown($event, index)"
            >
              <span class="term-tab-status" :class="`is-${tab.status}`" aria-hidden="true" />
              <span class="term-tab-label">{{ tab.title }}</span>
              <span
                class="term-tab-close"
                aria-hidden="true"
                :title="t('terminal.window.closeTab', { title: tab.title })"
                @click.stop="requestCloseTab(tab.id)"
              >
                <X :size="12" :stroke-width="2" aria-hidden="true" />
              </span>
            </button>
          </div>
        </div>
        <TerminalTargetPicker surface="window" class="term-window-add" />
      </div>
    </template>

    <div class="term-window-body">
      <TerminalSurface
        v-for="tab in tabs"
        v-show="tab.id === activeTab?.id"
        :key="tab.id"
        :tab-id="tab.id"
        variant="window"
        :labelled-by="`term-tab-${tab.id}`"
        :id="`term-panel-${tab.id}`"
      />
    </div>
  </FloatingWindow>

  <TerminalCloseDialog @confirmed="onConfirmed" />
</template>

<style scoped>
.term-window {
  position: fixed;
  right: var(--space-6);
  bottom: var(--space-4);
  z-index: var(--term-window-z, 68);
  width: min(55rem, calc(100vw - var(--space-6) * 2));
  height: min(37.5rem, calc(100vh - var(--term-window-chrome)));
  height: min(37.5rem, calc(100dvh - var(--term-window-chrome)));
  min-width: min(28.75rem, calc(100vw - var(--space-6) * 2));
  min-height: min(18.75rem, calc(100vh - var(--space-6) * 2));
  max-width: calc(100vw - var(--space-6) * 2);
  max-height: calc(100vh - var(--space-6) * 2);
  max-height: calc(100dvh - var(--space-6) * 2);
}

.term-window-context {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--muted);
  font-size: var(--text-xs);
}

.term-tabstrip {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-1);
  min-width: 0;
  padding: var(--space-1) var(--space-2) var(--space-1) var(--space-3);
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}

.term-tabs {
  flex: 0 1 auto;
  display: flex;
  align-items: center;
  gap: var(--space-1);
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: none;
}

.term-tabs::-webkit-scrollbar {
  display: none;
}

.term-tab {
  display: inline-flex;
  align-items: center;
  min-width: 0;
  max-width: min(14rem, 45vw);
  border-radius: var(--radius-sm);
  background: var(--surface-warm);
  color: var(--muted);
}

.term-tab.is-active {
  background: var(--bg);
  color: var(--fg-2);
}

.term-tab-name {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  min-width: 0;
  max-width: min(12rem, 40vw);
  padding: var(--space-1) var(--space-2);
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  font-size: var(--text-xs);
  cursor: pointer;
  white-space: nowrap;
}

.term-tab-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.term-tab-name:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
  border-radius: var(--radius-sm);
}

.term-tab-status {
  width: var(--space-1);
  height: var(--space-1);
  border-radius: var(--radius-pill);
  flex: 0 0 auto;
  background: var(--meta);
}

.term-tab-status.is-connected {
  background: var(--success);
}

.term-tab-status.is-connecting {
  background: var(--accent);
}

.term-tab-status.is-exited,
.term-tab-status.is-disconnected {
  background: var(--warn);
}

.term-tab-status.is-error {
  background: var(--danger);
}

.term-tab-close {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: var(--space-6);
  height: var(--space-6);
  margin-right: calc(-1 * var(--space-1));
  padding: 0;
  border: 0;
  background: transparent;
  border-radius: var(--radius-sm);
  color: var(--meta);
  cursor: pointer;
}

.term-tab-close:hover {
  background: var(--border);
  color: var(--fg-2);
}

.term-window-body {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  display: flex;
}

.term-window-body > :deep(.term-surface) {
  flex: 1 1 auto;
  min-width: 0;
}

@media (max-width: 640px) {
  .term-window {
    right: var(--space-4);
    bottom: var(--space-2);
  }

  .term-tab-status {
    display: none;
  }

  .term-tab {
    max-width: min(8.5rem, 42vw);
  }

  .term-tab-name {
    max-width: min(7rem, 36vw);
  }
}

@media (prefers-reduced-motion: reduce) {
  .term-window {
    transition: none;
  }
}
</style>
