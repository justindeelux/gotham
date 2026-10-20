<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import TerminalMetaBar from "@/features/terminal/components/TerminalMetaBar.vue";
import TerminalStatusBar from "@/features/terminal/components/TerminalStatusBar.vue";
import TerminalStatusOverlay from "@/features/terminal/components/TerminalStatusOverlay.vue";
import TerminalSuggestionStrip from "@/features/terminal/components/TerminalSuggestionStrip.vue";
import { useQuickCommandsStore } from "@/features/terminal/stores/quickCommands";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalTab } from "@/features/terminal/stores/terminal";
import type { TerminalCommandFailure } from "@/features/terminal/transport/terminalRuntime";

/**
 * Shared terminal surface (JUS-245): the one place xterm is displayed. Page
 * mode and the floating window both mount this component; it merely offers
 * its element to the tab's runtime, which owns the renderer, so moving between
 * the two re-parents the live buffer instead of reconnecting.
 *
 * JUS-249 splits the meta bar, status bar, lifecycle overlay and suggestion
 * strip into focused components; this file owns only the shared surface and
 * the keyboard boundary.
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
const quickCommands = useQuickCommandsStore();
const surface = ref<HTMLElement | null>(null);
const suggestionStrip = ref<{ focusOption: () => void } | null>(null);
const commandNotice = ref("");
const hintId = computed<string>(() => `term-hint-${props.tabId}`);
const tab = computed<TerminalTab | null>(() => terminal.findTab(props.tabId));

/**
 * NO_PROMPT_MARKS_GRACE_MS lets a healthy shell deliver its first OSC 133
 * mark before the strip claims marks are unavailable. Without it the hint
 * flashes on every connect, while the shell is still printing its prompt.
 */
const NO_PROMPT_MARKS_GRACE_MS = 1500;
const noPromptMarksHint = ref(false);
let promptHintTimer: ReturnType<typeof setTimeout> | null = null;

/** clearPromptHintTimer stops the pending grace check. */
function clearPromptHintTimer(): void {
  if (promptHintTimer !== null) {
    clearTimeout(promptHintTimer);
    promptHintTimer = null;
  }
}

/**
 * The hint only appears after a connected session has stayed mark-less for
 * the grace period; a first prompt mark clears it immediately.
 */
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

/** suggestion is the single abbreviation match the runtime reports. */
const suggestion = computed(() => tab.value?.suggestion ?? null);

/** hasAbbreviations chooses the fixed bar's setup vs usage hint. */
const hasAbbreviations = computed<boolean>(() =>
  quickCommands.commands.some((command) => command.abbreviation !== undefined),
);

/** surfaceLabel names the region for screen readers. */
const surfaceLabel = computed<string>(() =>
  t("terminal.pane.detailLabel", {
    title: tab.value?.title ?? t("terminal.window.heading"),
  }),
);

/** insertFailureText maps a refused insertion onto localized guidance. */
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

/** acceptSuggestion replaces the abbreviation; it never presses Enter. */
function acceptSuggestion(): void {
  const result = terminal.acceptSuggestion(props.tabId);
  if (!result.ok) {
    commandNotice.value = insertFailureText(result.failure);
    return;
  }
  commandNotice.value = "";
  void nextTick(() => terminal.focus(props.tabId));
}

/** dismissSuggestion hides the strip and returns focus to the shell. */
function dismissSuggestion(): void {
  terminal.dismissSuggestion(props.tabId);
  commandNotice.value = "";
  void nextTick(() => terminal.focus(props.tabId));
}

/** focusSuggestions moves keyboard focus onto the suggestion option. */
function focusSuggestions(): void {
  void nextTick(() => suggestionStrip.value?.focusOption());
}

/** returnFocus leaves the explicitly focused list without dismissing it. */
function returnFocus(): void {
  void nextTick(() => terminal.focus(props.tabId));
}

/** onMetaInserted announces a manual insert and lands focus in the shell. */
function onMetaInserted(label: string): void {
  commandNotice.value = t("terminal.quickCommands.inserted", { label });
  void nextTick(() => terminal.focus(props.tabId));
}

/** onMetaClosed returns focus to the shell after the quick-command modal. */
function onMetaClosed(): void {
  void nextTick(() => terminal.focus(props.tabId));
}

/** onCompositionStart suspends suggestions during IME composition. */
function onCompositionStart(): void {
  terminal.setComposing(props.tabId, true);
}

/** onCompositionEnd keeps the mirror suspended until the next prompt mark. */
function onCompositionEnd(): void {
  terminal.setComposing(props.tabId, false);
}

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

/** retrySession starts a new PTY; it never replays prior input. */
function retrySession(): void {
  void terminal.retry(props.tabId);
}

/** isTerminalKeyTarget keeps shortcuts out of the meta bar and popovers. */
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

/**
 * onSurfaceKeydown handles two non-shell chords:
 * - Ctrl+Shift+E leaves xterm's input trap and focuses the window tab or page
 *   action. Windows reserves Ctrl+Shift+Escape for Task Manager, so the chord
 *   deliberately avoids it; Shift+Tab also leaves the shell.
 * - Ctrl+Shift+Space focuses a visible suggestion list from the terminal area.
 * Esc, Ctrl+Space, Tab and Enter are never intercepted here; they reach the
 * PTY unless the suggestion list is explicitly focused.
 */
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

onMounted(attachSurface);
onBeforeUnmount(() => {
  clearPromptHintTimer();
  const element = surface.value;
  if (element) {
    terminal.detach(props.tabId, element);
  }
});
watch(() => props.tabId, attachSurface);
// If the match disappears (prompt ended, Esc, paste), never leave focus
// stranded on a removed option.
watch(suggestion, (next) => {
  if (next !== null) {
    return;
  }
  const active = document.activeElement;
  if (active instanceof HTMLElement && active.closest(".term-suggest")) {
    void nextTick(() => terminal.focus(props.tabId));
  }
});
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
    <TerminalSuggestionStrip
      ref="suggestionStrip"
      :suggestion="suggestion"
      :notice="commandNotice"
      :has-abbreviations="hasAbbreviations"
      :no-prompt-marks-hint="noPromptMarksHint"
      @accept="acceptSuggestion"
      @dismiss="dismissSuggestion"
      @dismiss-notice="commandNotice = ''"
      @return-focus="returnFocus"
    />
    <div ref="surface" class="term-screen" data-testid="terminal-screen" />

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

/* The light renderer gets a matching frame instead of a dark gutter around
 * the xterm canvas. The meta/status bars stay app chrome. */
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
</style>
