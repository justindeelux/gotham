<script setup lang="ts">
import { Minus, X } from "@lucide/vue";
import { NButton } from "naive-ui";
import { computed } from "vue";

import type { WindowBox, WindowResizeLimits } from "@/shared/window/geometry";
import {
  RESIZE_GRIPS,
  useWindowInteraction,
} from "@/shared/window/useWindowInteraction";

/**
 * Shared floating-window frame (JUS-275).
 *
 * The code editor and the terminal are both floating tools: the same frame,
 * title bar with minimize/close, eight resize grips and a bound content area.
 * This component owns that chrome and the drag/resize/focus mechanics; the
 * feature supplies its identity, its extra header actions and its body, and
 * keeps its own store, persistence keys and close/confirmation path.
 *
 * `classNames` exists so each feature keeps its existing selectors (tests and
 * e2e rely on `.ed-window-min`, `.term-resize--se`, ...) on the shared markup.
 */
interface FloatingWindowClassNames {
  bar?: string;
  min?: string;
  close?: string;
  resize?: string;
}

interface Props {
  windowLabel: string;
  minimizeLabel: string;
  closeLabel: string;
  resizeLabel: string;
  resizeValueText?: string;
  /** size is the displayed window size (see the feature store's fit). */
  size: { width: number; height: number };
  resizeLimits: WindowResizeLimits;
  classNames?: FloatingWindowClassNames;
}

const props = withDefaults(defineProps<Props>(), {
  resizeValueText: "",
  classNames: () => ({}),
});
const emit = defineEmits<{
  minimize: [];
  close: [];
  drag: [x: number, y: number];
  resize: [box: WindowBox];
  resizeBy: [deltaWidth: number, deltaHeight: number];
}>();

const { frame, ...interaction } = useWindowInteraction({
  size: () => props.size,
  limits: () => props.resizeLimits,
  onDrag: (x, y) => emit("drag", x, y),
  onResize: (box) => emit("resize", box),
  onResizeBy: (deltaWidth, deltaHeight) =>
    emit("resizeBy", deltaWidth, deltaHeight),
});

/** resizeAria keeps the keyboard slider honest on phone-sized viewports. */
const resizeAria = computed(() => {
  const min = Math.min(props.resizeLimits.minWidth, props.size.width);
  return {
    min,
    max: Math.max(min, props.resizeLimits.viewportWidth),
    now: props.size.width,
  };
});

/** resizeClass / min / close keep each feature's existing selectors. */
const resizeClass = computed<string>(
  () => props.classNames.resize ?? "floating-window-resize",
);
</script>

<template>
  <section
    ref="frame"
    class="floating-window"
    role="region"
    :aria-label="props.windowLabel"
  >
    <header
      class="floating-window-bar"
      :class="props.classNames.bar"
      @pointerdown="interaction.onBarPointerDown"
      @pointermove="interaction.onBarPointerMove"
      @pointerup="interaction.onBarPointerUp"
      @pointercancel="interaction.onBarPointerUp"
      @lostpointercapture="interaction.onBarPointerUp"
    >
      <span class="floating-window-title"><slot name="title" /></span>
      <span class="floating-window-context"><slot name="context" /></span>
      <span class="grow" />
      <div class="floating-window-actions"><slot name="actions" /></div>
      <div class="floating-window-controls">
        <NButton
          quaternary
          size="tiny"
          :class="props.classNames.min"
          :aria-label="props.minimizeLabel"
          @click="emit('minimize')"
        >
          <template #icon>
            <Minus :size="16" :stroke-width="1.8" aria-hidden="true" />
          </template>
        </NButton>
        <NButton
          quaternary
          size="tiny"
          :class="props.classNames.close"
          :aria-label="props.closeLabel"
          @click="emit('close')"
        >
          <template #icon>
            <X :size="16" :stroke-width="1.8" aria-hidden="true" />
          </template>
        </NButton>
      </div>
    </header>

    <slot name="tabs" />

    <div class="floating-window-body"><slot /></div>

    <!-- The SE grip carries the slider semantics (keyboard resize); the other
         grips are pointer-only. -->
    <div
      :class="[
        resizeClass,
        `${resizeClass}--se`,
        'floating-window-resize',
        'floating-window-resize--se',
      ]"
      tabindex="0"
      role="slider"
      :aria-label="props.resizeLabel"
      :aria-valuemin="resizeAria.min"
      :aria-valuemax="resizeAria.max"
      :aria-valuenow="resizeAria.now"
      :aria-valuetext="props.resizeValueText || undefined"
      aria-keyshortcuts="ArrowUp ArrowDown ArrowLeft ArrowRight"
      @pointerdown="interaction.onResizePointerDown($event, 'se')"
      @pointermove="interaction.onResizePointerMove"
      @pointerup="interaction.onResizePointerUp"
      @pointercancel="interaction.onResizePointerUp"
      @lostpointercapture="interaction.onResizePointerUp"
      @keydown="interaction.onResizeKeydown"
    />
    <div
      v-for="direction in RESIZE_GRIPS"
      :key="direction"
      :class="[
        resizeClass,
        `${resizeClass}--${direction}`,
        'floating-window-resize',
        `floating-window-resize--${direction}`,
      ]"
      aria-hidden="true"
      @pointerdown="interaction.onResizePointerDown($event, direction)"
      @pointermove="interaction.onResizePointerMove"
      @pointerup="interaction.onResizePointerUp"
      @pointercancel="interaction.onResizePointerUp"
      @lostpointercapture="interaction.onResizePointerUp"
    />
  </section>
</template>

<style scoped>
.floating-window {
  position: fixed;
  right: var(--space-6);
  bottom: var(--space-4);
  display: flex;
  flex-direction: column;
  background: var(--surface);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  /* The shared elevation (JUS-270/JUS-275): it paints outside the border box,
     so the frame's own overflow clip never trims it. */
  box-shadow: var(--elev-floating);
  overflow: hidden;
}

.floating-window-bar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-height: calc(var(--space-8) + var(--space-2));
  padding: var(--space-1) var(--space-2) var(--space-1) var(--space-4);
  border-bottom: 1px solid var(--border);
  background: var(--surface-warm);
  cursor: grab;
  user-select: none;
  touch-action: none;
}

.floating-window-bar:active {
  cursor: grabbing;
}

.floating-window-title {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-weight: 600;
  color: var(--fg-2);
  white-space: nowrap;
}

.floating-window-context {
  display: flex;
  flex: 0 1 auto;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  overflow: hidden;
}

.floating-window-bar .grow {
  flex: 1 1 auto;
  min-width: 0;
}

.floating-window-actions,
.floating-window-controls {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: var(--space-1);
}

/* The body is the only flexible region: the chrome always stays reachable. */
.floating-window-body {
  flex: 1 1 auto;
  display: flex;
  min-width: 0;
  min-height: 0;
}

.floating-window-body > :slotted(*) {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
}

/* Invisible hit strips on the frame; only the resize cursor signals them. */
.floating-window-resize {
  position: absolute;
  z-index: 5;
  touch-action: none;
}

.floating-window-resize--n {
  top: 0;
  left: var(--space-4);
  right: var(--space-4);
  height: var(--space-1);
  cursor: ns-resize;
}

.floating-window-resize--s {
  bottom: 0;
  left: var(--space-4);
  right: var(--space-4);
  height: var(--space-1);
  cursor: ns-resize;
}

.floating-window-resize--e {
  top: var(--space-4);
  bottom: var(--space-4);
  right: 0;
  width: var(--space-1);
  cursor: ew-resize;
}

.floating-window-resize--w {
  top: var(--space-4);
  bottom: var(--space-4);
  left: 0;
  width: var(--space-1);
  cursor: ew-resize;
}

.floating-window-resize--nw,
.floating-window-resize--ne,
.floating-window-resize--sw,
.floating-window-resize--se {
  width: var(--space-4);
  height: var(--space-4);
}

.floating-window-resize--nw {
  top: 0;
  left: 0;
  cursor: nwse-resize;
}

.floating-window-resize--ne {
  top: 0;
  right: 0;
  cursor: nesw-resize;
}

.floating-window-resize--sw {
  bottom: 0;
  left: 0;
  cursor: nesw-resize;
}

.floating-window-resize--se {
  right: 0;
  bottom: 0;
  cursor: nwse-resize;
}

.floating-window-resize:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
  border-radius: var(--radius-sm);
}
</style>
