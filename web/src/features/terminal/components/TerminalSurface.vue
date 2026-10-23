<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { CSSProperties } from "vue";
import { useI18n } from "vue-i18n";

import TerminalMetaBar from "@/features/terminal/components/TerminalMetaBar.vue";
import TerminalStatusBar from "@/features/terminal/components/TerminalStatusBar.vue";
import TerminalStatusOverlay from "@/features/terminal/components/TerminalStatusOverlay.vue";
import TerminalSuggestionStrip from "@/features/terminal/components/TerminalSuggestionStrip.vue";
import { useQuickCommandsStore } from "@/features/terminal/stores/quickCommands";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalTab } from "@/features/terminal/stores/terminal";
import type { TerminalCommandFailure } from "@/features/terminal/transport/terminalRuntime";

interface Props {
  tabId: string;
  variant: "page" | "window";
  labelledBy?: string;
}

const props = withDefaults(defineProps<Props>(), { labelledBy: "" });
const { t } = useI18n();
const terminal = useTerminalStore();
const quickCommands = useQuickCommandsStore();
const surface = ref<HTMLElement | null>(null);
const suggestionStrip = ref<{ focusOption: () => void } | null>(null);
const commandNotice = ref("");
const terminalBox = ref<HTMLElement | null>(null);
const hintId = computed<string>(() => `term-hint-${props.tabId}`);
const tab = computed<TerminalTab | null>(() => terminal.findTab(props.tabId));

const NO_PROMPT_MARKS_GRACE_MS = 1500;
const noPromptMarksHint = ref(false);
let promptHintTimer: ReturnType<typeof setTimeout> | null = null;

function clearPromptHintTimer(): void {
  if (promptHintTimer !== null) {
    clearTimeout(promptHintTimer);
    promptHintTimer = null;
  }
}

watch(
  () => [tab.value?.status, tab.value?.integrationDetected] as const,
  ([status, detected]) => {
    clearPromptHintTimer();
    noPromptMarksHint.value = false;
    if (status !== "connected" || detected) {
      return;
    }
    promptHintTimer = setTimeout(() => {
      promptHintTimer = null;
      if (tab.value?.status === "connected" && !tab.value?.integrationDetected) {
        noPromptMarksHint.value = true;
      }
    }, NO_PROMPT_MARKS_GRACE_MS);
  },
  { immediate: true },
);

const suggestion = computed(() => tab.value?.suggestion ?? null);

const hasAbbreviations = computed<boolean>(() =>
  quickCommands.commands.some((command) => command.abbreviation !== undefined),
);

const suggestionBarVisible = computed<boolean>(
  () =>
    suggestion.value !== null ||
    commandNotice.value !== "" ||
    (hasAbbreviations.value && noPromptMarksHint.value),
);

const SUGGESTION_STRIP_HEIGHT = 40;

const cursorRow = ref<{ top: number; height: number } | null>(null);
const terminalBoxHeight = ref<number>(0);
let stopCursorWatch: (() => void) | null = null;

function updateCursorRow(): void {
  const box = terminalBox.value;
  if (!box) {
    cursorRow.value = null;
    return;
  }
  const boxRect = box.getBoundingClientRect();
  terminalBoxHeight.value = boxRect.height;
  const cursor = terminal.cursorRow(props.tabId);
  cursorRow.value = cursor
    ? { top: cursor.top - boxRect.top, height: cursor.height }
    : null;
}

function stopCursorTracking(): void {
  stopCursorWatch?.();
  stopCursorWatch = null;
}

function startCursorTracking(): void {
  stopCursorTracking();
  updateCursorRow();
  stopCursorWatch = terminal.watchCursor(props.tabId, updateCursorRow);
}

const stripStyle = computed<CSSProperties>(() => {
  const cursor = cursorRow.value;
  if (!cursor) {
    return { top: "0px" };
  }
  const height = SUGGESTION_STRIP_HEIGHT;
  const below = cursor.top + cursor.height;
  if (cursor.top >= height) {
    return { top: `${Math.round(cursor.top - height)}px` };
  }
  if (terminalBoxHeight.value - below >= height) {
    return { top: `${Math.round(below)}px` };
  }
  return cursor.top > terminalBoxHeight.value - below
    ? { top: "0px" }
    : { top: `${Math.max(0, Math.round(terminalBoxHeight.value - height))}px` };
});

const surfaceLabel = computed<string>(() =>
  t("terminal.pane.detailLabel", {
    title: tab.value?.title ?? t("terminal.window.heading"),
  }),
);

function insertFailureText(failure?: TerminalCommandFailure): string {
  switch (failure) {
    case "multiline-no-paste":
      return t("terminal.quickCommands.insertMultiline");
    case "control":
      return t("terminal.quickCommands.insertControl");
    case "empty":
      return t("terminal.quickCommands.insertEmpty");
    default:
      return t("terminal.quickCommands.insertUnavailable");
  }
}

function acceptSuggestion(): void {
  const result = terminal.acceptSuggestion(props.tabId);
  if (!result.ok) {
    commandNotice.value = insertFailureText(result.failure);
    return;
  }
  commandNotice.value = "";
  void nextTick(() => terminal.focus(props.tabId));
}

function dismissSuggestion(): void {
  terminal.dismissSuggestion(props.tabId);
  commandNotice.value = "";
  void nextTick(() => terminal.focus(props.tabId));
}

function focusSuggestions(): void {
  void nextTick(() => suggestionStrip.value?.focusOption());
}

function returnFocus(): void {
  void nextTick(() => terminal.focus(props.tabId));
}

function onMetaInserted(label: string): void {
  commandNotice.value = t("terminal.quickCommands.inserted", { label });
  void nextTick(() => terminal.focus(props.tabId));
}

function onMetaClosed(): void {
  void nextTick(() => terminal.focus(props.tabId));
}

function onCompositionStart(): void {
  terminal.setComposing(props.tabId, true);
}

function onCompositionEnd(): void {
  terminal.setComposing(props.tabId, false);
}

function attachSurface(): void {
  const element = surface.value;
  if (element) {
    terminal.attach(props.tabId, element);
  }
}

function focusTerminal(): void {
  terminal.focus(props.tabId);
}

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

function retrySession(): void {
  void terminal.retry(props.tabId);
}

function isTerminalKeyTarget(event: KeyboardEvent): boolean {
  const target = event.target;
  if (!(target instanceof Element)) {
    return false;
  }
  return (
    target.closest(".term-surface") !== null &&
    target.closest(".term-meta") === null &&
    target.closest(".n-popover") === null
  );
}

function onSurfaceKeydown(event: KeyboardEvent): void {
  if (
    event.code === "Space" &&
    event.ctrlKey &&
    event.shiftKey &&
    !event.altKey &&
    !event.metaKey &&
    suggestion.value !== null &&
    isTerminalKeyTarget(event)
  ) {
    event.preventDefault();
    event.stopPropagation();
    focusSuggestions();
    return;
  }
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

function onWindowResize(): void {
  if (suggestionBarVisible.value) {
    void nextTick(updateCursorRow);
  }
}

onMounted(() => {
  attachSurface();
  window.addEventListener("resize", onWindowResize);
});
onBeforeUnmount(() => {
  clearPromptHintTimer();
  stopCursorTracking();
  window.removeEventListener("resize", onWindowResize);
  const element = surface.value;
  if (element) {
    terminal.detach(props.tabId, element);
  }
});
watch(() => props.tabId, attachSurface);
watch(suggestion, (next) => {
  if (next !== null) {
    return;
  }
  const active = document.activeElement;
  if (active instanceof HTMLElement && active.closest(".term-suggest")) {
    void nextTick(() => terminal.focus(props.tabId));
  }
});
watch(
  () => tab.value?.sessionId,
  () => {
    if (tab.value?.sessionId) {
      void nextTick(attachSurface);
    }
  },
);
watch(suggestionBarVisible, (visible) => {
  if (visible) {
    void nextTick(startCursorTracking);
    return;
  }
  stopCursorTracking();
  cursorRow.value = null;
});
watch(
  () => [tab.value?.rows, tab.value?.cols] as const,
  () => {
    if (suggestionBarVisible.value) {
      void nextTick(updateCursorRow);
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
    aria-keyshortcuts="Control+Shift+E Control+Shift+Space"
    :data-theme="terminal.theme"
    @click="onSurfaceClick"
    @keydown.capture="onSurfaceKeydown"
    @compositionstart.capture="onCompositionStart"
    @compositionend.capture="onCompositionEnd"
  >
    <TerminalMetaBar
      :tab="tab"
      :variant="variant"
      @inserted="onMetaInserted"
      @focus-terminal="onMetaClosed"
    />

    <p :id="hintId" class="term-sr">
      {{ t("terminal.shortcuts.hint") }}
    </p>
    <div ref="terminalBox" class="term-terminal">
      <div ref="surface" class="term-screen" data-testid="terminal-screen" />
      <TerminalSuggestionStrip
        v-if="suggestionBarVisible"
        ref="suggestionStrip"
        :suggestion="suggestion"
        :notice="commandNotice"
        :no-prompt-marks-hint="noPromptMarksHint"
        :style="stripStyle"
        @accept="acceptSuggestion"
        @dismiss="dismissSuggestion"
        @dismiss-notice="commandNotice = ''"
        @return-focus="returnFocus"
      />
    </div>

    <TerminalStatusOverlay :tab="tab" @retry="retrySession" />
    <TerminalStatusBar :tab="tab" :variant="variant" />
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

.term-surface[data-theme="gotham-light"] {
  background: var(--term-light-bg);
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

.term-terminal {
  position: relative;
  display: flex;
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
}

.term-screen {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
  padding: var(--space-2);
  overflow: hidden;
}

.term-screen :deep(.term-host) {
  width: 100%;
  height: 100%;
}
</style>
