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

/**
 * App-level floating terminal window (JUS-245).
 *
 * It mounts once in AppLayout (next to the code editor) and collapses with
 * `v-show`, so every PTY survives route navigation, tab switches and
 * minimize/restore. Multiple tabs are independent sessions; the geometry is
 * the same shared math the editor uses (move, eight grips, viewport bound).
 */
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

/** displayedSize is the fitted size for the current viewport. */
const displayedSize = computed<{ width: number; height: number }>(() => {
  const fitted = windowStore.displayGeometry(
    viewport.value.width,
    viewport.value.height,
  );
  return { width: fitted.width, height: fitted.height };
});

/** resizeLimits is the terminal's resize band for the current viewport. */
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

/** windowStyle applies the displayed size and the dragged position. */
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

/** onWindowResize records the viewport for display-only fitting. */
function onWindowResize(): void {
  viewport.value = { width: window.innerWidth, height: window.innerHeight };
}

/** focusWindow raises this window above the editor until focus leaves it. */
function focusWindow(): void {
  windowStore.raise();
}

/** onDocumentPointerDown lowers the window when a click lands elsewhere. */
function onDocumentPointerDown(event: PointerEvent): void {
  if (
    !windowStore.raised ||
    (event.target instanceof Element && event.target.closest(".term-window"))
  ) {
    return;
  }
  windowStore.lower();
}

/** onWindowDrag applies a title-bar drag, clamped by the store. */
function onWindowDrag(x: number, y: number): void {
  windowStore.setPosition(
    x,
    y,
    displayedSize.value.width,
    displayedSize.value.height,
  );
}

/** onWindowResize applies a grip resize; the store keeps the opposite edge. */
function onWindowResizeMove(box: WindowBox): void {
  windowStore.resizeTo(box, viewport.value.width, viewport.value.height);
}

/** onWindowResizeBy applies one keyboard resize step from the slider. */
function onWindowResizeBy(deltaWidth: number, deltaHeight: number): void {
  windowStore.resizeBy(
    deltaWidth,
    deltaHeight,
    viewport.value.width,
    viewport.value.height,
  );
}

/** minimizeWindow collapses into the rail and moves focus to its icon. */
function minimizeWindow(): void {
  windowStore.setMinimized(true);
  void nextTick(() => {
    document
      .querySelector<HTMLElement>(".rail-btn--terminal")
      ?.focus();
  });
}

/** restoreWindow reopens the window and re-fits the active renderer. */
function restoreWindow(): void {
  windowStore.setMinimized(false);
}

/** focusRestoredWindow lands focus on the active tab after a restore. */
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

// Switching tabs re-measures the newly visible xterm host and keeps focus on
// the tab the user chose.
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

/** requestCloseTab uses the shared confirmation path. */
function requestCloseTab(id: string): void {
  terminal.requestClose([id]);
}

/** requestCloseWindow confirms all window tabs. */
function requestCloseWindow(): void {
  terminal.requestClose(tabs.value.map((tab) => tab.id));
}

/** activateTab selects one tab and reveals its panel. */
function activateTab(id: string): void {
  terminal.activate(id);
}

/** focusTab moves focus to one tab button (roving tablist). */
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

/** revealActiveTab keeps the active tab visible in a scrolling strip. */
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

/** onTabKeydown implements roving focus and the Delete close path. */
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

/**
 * onGlobalKeydown owns the terminal-wide shortcuts. They deliberately avoid
 * Alt+W (the editor's close-tab binding) and are scoped to focus inside the
 * window/page chrome, never inside the live shell: readline, vim and tmux
 * need their Meta bindings, and xterm's `macOptionIsMeta` turns Option into
 * Meta instead of a composed character. Alt+Shift+W closes the window; Alt+M
 * minimizes it; both go through the dialog/document path and are listed in
 * the surface shortcuts popover.
 */
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
  // The xterm textarea (and its helper nodes) owns Alt/Option chords while
  // the shell has focus, so vim/tmux Meta bindings reach the PTY.
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

/** onConfirmed returns focus after the dialog removes its trigger. */
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
    // Nothing terminal is left: prefer the dock/rail control if one still
    // renders instead of an arbitrary rail tile.
    const fallback =
      document.querySelector<HTMLElement>(".term-dock-resume") ??
      document.querySelector<HTMLElement>(".rail-btn--terminal") ??
      document.querySelector<HTMLElement>(".rail .rail-btn");
    fallback?.focus();
  });
}

/** onBeforeUnload warns when a browser unload would kill a live PTY. */
function onBeforeUnload(event: BeforeUnloadEvent): void {
  if (!terminal.hasLiveSessions) {
    return;
  }
  event.preventDefault();
  event.returnValue = "";
}

// A newly opened window tab (from a page transfer or an entry point) restores
// the window without stealing focus from the pane that requested it.
watch(
  () => tabs.value.length,
  (count, previous) => {
    if (count > (previous ?? 0)) {
      // A new session (entry point or page transfer) comes to the front.
      windowStore.raise();
    }
    if (count > (previous ?? 0) && (previous === 0 || windowStore.minimized)) {
      windowStore.setMinimized(false);
    }
  },
);

onMounted(() => {
  window.addEventListener("resize", onWindowResize);
  // Capture phase: xterm consumes some modified keys before they bubble, so
  // the documented terminal chords must see them first.
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

// Expose restore for the rail integration; the rail toggles the same method.
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

    <!-- JUS-275: sessions live on their own strip below the title bar, with
         the add-tab (+) control directly after the last tab. The strip scrolls
         instead of pushing minimize/close or the add control off-screen. -->
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
            <!-- The tablist owns only role=tab elements: the close control is a
                 non-interactive span inside the tab (like the file tabs), so
                 aria-required-children stays valid. Keyboard users close the
                 focused tab with Delete, which the tab announces. -->
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

/* The identity copy in the shared title bar: the heading already names the
   tool, so the active session's title follows it as context. */
.term-window-context {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--muted);
  font-size: var(--text-xs);
}

/* JUS-275: sessions sit on their own row under the title bar. The strip takes
   the free width, scrolls when tabs overflow, and keeps the add-tab control
   immediately after the last tab. */
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




/* JUS-261: the window controls are pinned to the far right edge in every
   state. The flexible space sits before them, so the tab strip can grow (or
   scroll) without ever pushing minimize/close inward, and the add-tab slot
   stays directly after the tabs. */

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

  /* A phone keeps the tab labels legible: the status dots yield. */
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
