<script setup lang="ts">
import { Minus, X } from "@lucide/vue";
import { NButton } from "naive-ui";
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
import {
  TERMINAL_WINDOW_CHROME_MARGIN,
  TERMINAL_WINDOW_MIN_HEIGHT,
  TERMINAL_WINDOW_MIN_WIDTH,
  TERMINAL_WINDOW_SIDE_MARGIN,
} from "@/features/terminal/schemas/terminal";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import { useTerminalWindowStore } from "@/features/terminal/stores/terminalWindow";
import {
  WINDOW_RESIZE_DIRECTIONS,
  resizeWindowBox,
  windowResizeLimits,
} from "@/shared/window/geometry";
import type { WindowResizeDirection } from "@/shared/window/geometry";

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

const windowElement = ref<HTMLElement | null>(null);
const tabsElement = ref<HTMLElement | null>(null);
const viewport = ref({
  width: typeof window === "undefined" ? 0 : window.innerWidth,
  height: typeof window === "undefined" ? 0 : window.innerHeight,
});

let dragState: {
  pointerId: number;
  startX: number;
  startY: number;
  originX: number;
  originY: number;
} | null = null;

let resizeState: {
  pointerId: number;
  direction: WindowResizeDirection;
  startX: number;
  startY: number;
  originX: number;
  originY: number;
  originWidth: number;
  originHeight: number;
} | null = null;

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

/** resizeAria carries honest slider semantics for the keyboard grip. */
const resizeAria = computed<{ min: number; max: number; now: number }>(() => {
  const min = Math.min(TERMINAL_WINDOW_MIN_WIDTH, displayedSize.value.width);
  return {
    min,
    max: Math.max(min, viewport.value.width),
    now: displayedSize.value.width,
  };
});

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
  const element = windowElement.value;
  if (
    !windowStore.raised ||
    !element ||
    (event.target instanceof Node && element.contains(event.target))
  ) {
    return;
  }
  windowStore.lower();
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

/** onBarPointerDown starts a title-bar drag (never on a control). */
function onBarPointerDown(event: PointerEvent): void {
  if (event.button !== 0) {
    return;
  }
  const bar = event.currentTarget;
  const element = windowElement.value;
  if (!(bar instanceof HTMLElement) || !element) {
    return;
  }
  // The tab strip and header controls own their clicks; a drag starts only on
  // the empty title-bar surface so pointer capture never swallows a tab close.
  if (
    event.target instanceof Element &&
    event.target.closest("button, .term-tabs")
  ) {
    return;
  }
  const rect = element.getBoundingClientRect();
  dragState = {
    pointerId: event.pointerId,
    startX: event.clientX,
    startY: event.clientY,
    originX: rect.left,
    originY: rect.top,
  };
  bar.setPointerCapture(event.pointerId);
  focusWindow();
}

/** onBarPointerMove drags by the pointer delta against the displayed size. */
function onBarPointerMove(event: PointerEvent): void {
  if (!dragState || event.pointerId !== dragState.pointerId) {
    return;
  }
  windowStore.setPosition(
    dragState.originX + (event.clientX - dragState.startX),
    dragState.originY + (event.clientY - dragState.startY),
    displayedSize.value.width,
    displayedSize.value.height,
  );
}

/** onBarPointerUp ends the drag (also on pointercancel). */
function onBarPointerUp(event: PointerEvent): void {
  if (dragState && event.pointerId === dragState.pointerId) {
    dragState = null;
  }
}

/** RESIZE_GRIPS are the pointer-only grips; the SE corner is the slider. */
const RESIZE_GRIPS = WINDOW_RESIZE_DIRECTIONS.filter(
  (direction) => direction !== "se",
);

/** onResizePointerDown seeds a resize from the rendered box. */
function onResizePointerDown(
  event: PointerEvent,
  direction: WindowResizeDirection,
): void {
  if (event.button !== 0) {
    return;
  }
  const handle = event.currentTarget;
  const element = windowElement.value;
  if (!(handle instanceof HTMLElement) || !element) {
    return;
  }
  const rect = element.getBoundingClientRect();
  resizeState = {
    pointerId: event.pointerId,
    direction,
    startX: event.clientX,
    startY: event.clientY,
    originX: rect.left,
    originY: rect.top,
    originWidth: displayedSize.value.width,
    originHeight: displayedSize.value.height,
  };
  handle.setPointerCapture(event.pointerId);
  event.preventDefault();
  focusWindow();
}

/** onResizePointerMove applies the grip delta inside the viewport band. */
function onResizePointerMove(event: PointerEvent): void {
  if (!resizeState || event.pointerId !== resizeState.pointerId) {
    return;
  }
  const next = resizeWindowBox(
    resizeState.direction,
    {
      x: resizeState.originX,
      y: resizeState.originY,
      width: resizeState.originWidth,
      height: resizeState.originHeight,
    },
    event.clientX - resizeState.startX,
    event.clientY - resizeState.startY,
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
  windowStore.resizeTo(next, viewport.value.width, viewport.value.height);
}

/** onResizePointerUp ends the resize (also on pointercancel). */
function onResizePointerUp(event: PointerEvent): void {
  if (resizeState && event.pointerId === resizeState.pointerId) {
    resizeState = null;
  }
}

/** RESIZE_KEY_STEP/LARGE mirror the editor's keyboard resize steps. */
const RESIZE_KEY_STEP = 16;
const RESIZE_KEY_STEP_LARGE = 64;

/** onResizeKeydown resizes from the keyboard slider grip. */
function onResizeKeydown(event: KeyboardEvent): void {
  const step = event.shiftKey ? RESIZE_KEY_STEP_LARGE : RESIZE_KEY_STEP;
  let deltaWidth = 0;
  let deltaHeight = 0;
  switch (event.key) {
    case "ArrowRight":
      deltaWidth = step;
      break;
    case "ArrowLeft":
      deltaWidth = -step;
      break;
    case "ArrowDown":
      deltaHeight = step;
      break;
    case "ArrowUp":
      deltaHeight = -step;
      break;
    default:
      return;
  }
  event.preventDefault();
  windowStore.resizeBy(
    deltaWidth,
    deltaHeight,
    viewport.value.width,
    viewport.value.height,
  );
}

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
 * Alt+W (the editor's close-tab binding) and are scoped to focus inside a
 * terminal surface/window, so typing Option+M in a page input is never eaten.
 * Alt+Shift+W closes the window; Alt+M minimizes it; both go through the
 * dialog/document path and are listed in the surface shortcuts popover.
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
    document.querySelector<HTMLElement>(".rail .rail-btn")?.focus();
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
  window.addEventListener("keydown", onGlobalKeydown);
  window.addEventListener("pointerdown", onDocumentPointerDown, true);
  window.addEventListener("beforeunload", onBeforeUnload);
  onWindowResize();
});

onBeforeUnmount(() => {
  window.removeEventListener("resize", onWindowResize);
  window.removeEventListener("keydown", onGlobalKeydown);
  window.removeEventListener("pointerdown", onDocumentPointerDown, true);
  window.removeEventListener("beforeunload", onBeforeUnload);
});

// Expose restore for the rail integration; the rail toggles the same method.
defineExpose({ restoreWindow });
</script>

<template>
  <section
    v-if="tabs.length > 0"
    v-show="!windowStore.minimized"
    ref="windowElement"
    class="term-window"
    :style="windowStyle"
    role="region"
    :aria-label="t('terminal.window.label')"
    @pointerdown="focusWindow"
    @focusin="focusWindow"
  >
    <header
      class="term-window-bar"
      @pointerdown="onBarPointerDown"
      @pointermove="onBarPointerMove"
      @pointerup="onBarPointerUp"
      @pointercancel="onBarPointerUp"
      @lostpointercapture="onBarPointerUp"
    >
      <span class="term-window-title">{{ t("terminal.window.heading") }}</span>
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
            {{ tab.title }}
          </button>
          <!-- A real button: Enter/Space closes, its label names the tab, and
               Delete on the focused tab is the alternative keyboard path. -->
          <button
            type="button"
            class="term-tab-close"
            :aria-label="t('terminal.window.closeTab', { title: tab.title })"
            @click.stop="requestCloseTab(tab.id)"
          >
            <X :size="12" :stroke-width="2" aria-hidden="true" />
          </button>
        </div>
      </div>
      <span class="grow" />
      <NButton
        quaternary
        size="tiny"
        class="term-window-min"
        :aria-label="t('terminal.window.minimize')"
        @click="minimizeWindow"
      >
        <template #icon>
          <Minus :size="16" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
      <NButton
        quaternary
        size="tiny"
        class="term-window-close"
        :aria-label="t('terminal.window.close')"
        @click="requestCloseWindow"
      >
        <template #icon>
          <X :size="16" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
    </header>

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

    <!-- The SE grip carries the slider semantics (keyboard resize); the other
         grips are pointer-only. -->
    <div
      class="term-resize term-resize--se"
      tabindex="0"
      role="slider"
      :aria-label="t('terminal.window.resize')"
      :aria-valuemin="resizeAria.min"
      :aria-valuemax="resizeAria.max"
      :aria-valuenow="resizeAria.now"
      aria-keyshortcuts="ArrowUp ArrowDown ArrowLeft ArrowRight"
      @pointerdown="onResizePointerDown($event, 'se')"
      @pointermove="onResizePointerMove"
      @pointerup="onResizePointerUp"
      @pointercancel="onResizePointerUp"
      @lostpointercapture="onResizePointerUp"
      @keydown="onResizeKeydown"
    />
    <div
      v-for="direction in RESIZE_GRIPS"
      :key="direction"
      class="term-resize"
      :class="`term-resize--${direction}`"
      aria-hidden="true"
      @pointerdown="onResizePointerDown($event, direction)"
      @pointermove="onResizePointerMove"
      @pointerup="onResizePointerUp"
      @pointercancel="onResizePointerUp"
      @lostpointercapture="onResizePointerUp"
    />
  </section>

  <TerminalCloseDialog @confirmed="onConfirmed" />
</template>

<style scoped>
.term-window {
  position: fixed;
  right: var(--space-6);
  bottom: var(--space-4);
  z-index: var(--term-window-z, 68);
  display: flex;
  flex-direction: column;
  width: min(55rem, calc(100vw - var(--space-6) * 2));
  height: min(37.5rem, calc(100vh - var(--term-window-chrome)));
  height: min(37.5rem, calc(100dvh - var(--term-window-chrome)));
  min-width: min(28.75rem, calc(100vw - var(--space-6) * 2));
  min-height: min(18.75rem, calc(100vh - var(--space-6) * 2));
  max-width: calc(100vw - var(--space-6) * 2);
  max-height: calc(100vh - var(--space-6) * 2);
  max-height: calc(100dvh - var(--space-6) * 2);
  background: var(--surface);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--elev-raised);
  overflow: hidden;
}

.term-window-bar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-height: calc(var(--space-8) + var(--space-2));
  padding: var(--space-1) var(--space-2);
  padding-left: var(--space-3);
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  cursor: grab;
  touch-action: none;
  user-select: none;
}

.term-window-bar:active {
  cursor: grabbing;
}

.term-window-title {
  font-weight: 600;
  color: var(--fg-2);
  white-space: nowrap;
}

.term-tabs {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  min-width: 0;
  max-width: 60%;
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
  overflow: hidden;
  text-overflow: ellipsis;
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
  place-items: center;
  width: var(--space-6);
  height: var(--space-6);
  margin-right: var(--space-1);
  padding: 0;
  border: 0;
  background: transparent;
  border-radius: var(--radius-sm);
  color: var(--meta);
  cursor: pointer;
}

.term-tab-close:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
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

.term-resize {
  position: absolute;
  z-index: 5;
  touch-action: none;
}

.term-resize--n {
  top: calc(-1 * var(--space-1) / 2);
  left: var(--space-3);
  right: var(--space-3);
  height: var(--space-1);
  cursor: ns-resize;
}

.term-resize--s {
  bottom: calc(-1 * var(--space-1) / 2);
  left: var(--space-3);
  right: var(--space-3);
  height: var(--space-1);
  cursor: ns-resize;
}

.term-resize--e {
  right: calc(-1 * var(--space-1) / 2);
  top: var(--space-3);
  bottom: var(--space-3);
  width: var(--space-1);
  cursor: ew-resize;
}

.term-resize--w {
  left: calc(-1 * var(--space-1) / 2);
  top: var(--space-3);
  bottom: var(--space-3);
  width: var(--space-1);
  cursor: ew-resize;
}

.term-resize--ne {
  top: calc(-1 * var(--space-1) / 2);
  right: calc(-1 * var(--space-1) / 2);
  width: var(--space-3);
  height: var(--space-3);
  cursor: nesw-resize;
}

.term-resize--nw {
  top: calc(-1 * var(--space-1) / 2);
  left: calc(-1 * var(--space-1) / 2);
  width: var(--space-3);
  height: var(--space-3);
  cursor: nwse-resize;
}

.term-resize--sw {
  bottom: calc(-1 * var(--space-1) / 2);
  left: calc(-1 * var(--space-1) / 2);
  width: var(--space-3);
  height: var(--space-3);
  cursor: nesw-resize;
}

.term-resize--se {
  bottom: 0;
  right: 0;
  width: var(--space-4);
  height: var(--space-4);
  cursor: nwse-resize;
}

.term-resize--se:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}

@media (max-width: 640px) {
  .term-window {
    right: var(--space-4);
    bottom: var(--space-2);
  }

  /* A phone keeps both tabs legible: the window title and status dots yield,
     leaving the tab labels (and their close buttons) the header width. */
  .term-window-title,
  .term-tab-status {
    display: none;
  }

  .term-tabs {
    max-width: calc(100% - var(--space-12));
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
