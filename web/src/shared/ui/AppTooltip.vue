<script lang="ts">
import { NTooltip } from "naive-ui";
import {
  cloneVNode,
  computed,
  defineComponent,
  h,
  mergeProps,
  nextTick,
  onBeforeUnmount,
  ref,
  watch,
} from "vue";
import type { PropType, VNode } from "vue";

/**
 * AppTooltip is the one tooltip behind every icon-only control in the Files UI.
 * It renders no wrapper of its own — the single slotted control is cloned in
 * place — so a button's layout and hit area are untouched.
 *
 * It opens for a pointer hover and for a keyboard-originated focus. naive's
 * NTooltip alone cannot: its `trigger` is a single value ("hover" xor "focus"),
 * so the tooltip here is driven through naive's controlled `show`
 * (trigger="manual") and the open/close state is wired to the child's own
 * events. The event policy is deliberate:
 *
 *   · hover   — a real pointer device only: not on (hover: none), and never for
 *               a touch tap's compatibility mouseenter, so a tap cannot leave a
 *               stuck tooltip (F4, round-2 G7);
 *   · focus   — a navigation key (Tab / arrow) moved focus here and the control
 *               matches :focus-visible, so a programmatic focus return (a dialog
 *               or dropdown handing focus back) does not pop it (F4);
 *   · Escape  — dismisses an open tooltip without swallowing the key, so an
 *               enclosing dialog/dropdown still receives it (F3, WCAG 1.4.13);
 *   · pointer — the tooltip stays open while the pointer crosses onto it and
 *               closes when it leaves either the control or the tooltip (F3).
 *
 * The popover is mounted LAZILY, on the control's first hover/focus (round-2
 * G5): a virtualised list can mount thousands of rows while scrolling, and a
 * naive popover per icon button made that measurably slower. Until then the
 * control is rendered alone, with the same listeners.
 *
 * A control whose tooltip copy differs from its accessible name (a disabled
 * reason) is described by an always-present, visually hidden node — so
 * assistive tech gets the reason even while no tooltip is open — and the visual
 * popover content is aria-hidden, so nothing is announced twice (F5 / round-2
 * G4).
 *
 * It keeps naive's `__popover__` contract and forwards the two internal props a
 * wrapping NDropdown sets. naive uses that flag to route the dropdown's own
 * trigger handlers through `internalInheritedEventHandlers` and to sync the
 * dropdown's target through this nested popover, so a dropdown's trigger button
 * carries its tooltip without breaking the dropdown's open/close or focus
 * return. Nothing else about naive's popover internals is relied on.
 */

/** HOVER_GAP_MS leaves a short window for the pointer to reach the tooltip. */
const HOVER_GAP_MS = 120;

/** SHOW_DELAY_MS is the hover/focus intent delay before a tooltip opens. */
const SHOW_DELAY_MS = 500;

/**
 * TOUCH_TAP_MS is the window just after a touch tap in which a compatibility
 * mouseenter is ignored: a tap fires mouseenter with no matching mouseleave, so
 * without this a hybrid (touch + mouse) device could leave a stuck tooltip.
 */
const TOUCH_TAP_MS = 600;

/** Keys that move focus; a focus right after one of these is user-intended. */
const NAVIGATION_KEYS = new Set([
  "Tab",
  "ArrowUp",
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
]);

/**
 * keyboardNavigation records whether the LAST input was a focus-navigation
 * keydown (as opposed to a pointer press). The document listeners are bound
 * once, on the first tooltip, and the flag is cleared on every focusin so a
 * stale value can never open a tooltip on a later programmatic focus.
 */
let keyboardNavigation = false;
/** lastTouchTapAt is when the last touch pointer went down (0 = never). */
let lastTouchTapAt = 0;
let modalityBound = false;
function bindInputModality(): void {
  if (modalityBound || typeof document === "undefined") {
    return;
  }
  modalityBound = true;
  document.addEventListener(
    "keydown",
    (event) => {
      keyboardNavigation = NAVIGATION_KEYS.has(event.key);
    },
    true,
  );
  document.addEventListener(
    "pointerdown",
    (event) => {
      keyboardNavigation = false;
      if (event.pointerType === "touch") {
        lastTouchTapAt = Date.now();
      }
    },
    true,
  );
  // Bubble phase: a tooltip's own focusin handler reads the flag first.
  document.addEventListener("focusin", () => {
    keyboardNavigation = false;
  });
}

/** tooltipSeq numbers each tooltip's describedby target. */
let tooltipSeq = 0;

const AppTooltip = defineComponent({
  name: "AppTooltip",
  inheritAttrs: false,
  props: {
    /** Tooltip copy; usually the control's i18n label, or a disabled reason. */
    label: { type: String, required: true },
    /** naive popover nesting contract, forwarded from a wrapping NDropdown.
     *  `never[]` because the wrapper never inspects these handlers. */
    internalSyncTargetWithParent: Boolean,
    internalInheritedEventHandlers: {
      type: Array as unknown as PropType<never[]>,
      default: () => [],
    },
  },
  setup(props, { slots }) {
    bindInputModality();
    const show = ref(false);
    /**
     * activated mounts the popover once, on the first hover/focus (G5). A
     * nested popover is the exception: when a wrapping NDropdown routes its own
     * trigger handlers through this tooltip (`internalInheritedEventHandlers`)
     * and syncs its target through it, the inner popover must exist from the
     * start or the dropdown can never open. Those are the few dropdown triggers
     * (the row ⋮, the sort toggles, New, the recycle purge), not the row
     * actions, so the virtualised scroll still mounts no extra popover per row.
     */
    const nested = computed(
      () => props.internalInheritedEventHandlers.length > 0,
    );
    const activated = ref(nested.value);
    watch(nested, (value) => {
      if (value) {
        activated.value = true;
      }
    });
    const contentId = `app-tooltip-${(tooltipSeq += 1)}`;
    let child: Element | { $el?: unknown } | null = null;
    let hoveringContent = false;
    let hideTimer: ReturnType<typeof setTimeout> | null = null;
    let showTimer: ReturnType<typeof setTimeout> | null = null;
    /** restoringFocus suppresses the focusout of an activation re-render. */
    let restoringFocus = false;

    function cancelHide(): void {
      if (hideTimer !== null) {
        clearTimeout(hideTimer);
        hideTimer = null;
      }
    }

    /** cancelShow drops a pending delayed open. */
    function cancelShow(): void {
      if (showTimer !== null) {
        clearTimeout(showTimer);
        showTimer = null;
      }
    }

    /** scheduleShow opens the tooltip after the intent delay. */
    function scheduleShow(): void {
      cancelHide();
      if (show.value || showTimer !== null) {
        return;
      }
      showTimer = setTimeout(() => {
        showTimer = null;
        show.value = true;
      }, SHOW_DELAY_MS);
    }

    function hideNow(): void {
      cancelShow();
      cancelHide();
      show.value = false;
    }

    /** hideSoon leaves HOVER_GAP_MS for the pointer to reach the tooltip. */
    function hideSoon(): void {
      cancelHide();
      hideTimer = setTimeout(() => {
        hideTimer = null;
        if (!hoveringContent) {
          show.value = false;
        }
      }, HOVER_GAP_MS);
    }

    /** hoverCapable is false on a touch-only device. */
    function hoverCapable(): boolean {
      return (
        typeof window !== "undefined" &&
        window.matchMedia("(hover: hover)").matches
      );
    }

    /** fromTouchTap reports a compatibility mouseenter synthesised by a tap. */
    function fromTouchTap(event: MouseEvent): boolean {
      const capabilities = (
        event as MouseEvent & {
          sourceCapabilities?: { firesTouchEvents?: boolean };
        }
      ).sourceCapabilities;
      return (
        capabilities?.firesTouchEvents === true ||
        Date.now() - lastTouchTapAt < TOUCH_TAP_MS
      );
    }

    /** childElement resolves the slotted control's DOM element for refocus. */
    function childElement(): HTMLElement | null {
      if (child instanceof HTMLElement) {
        return child;
      }
      const el = (child as { $el?: unknown } | null)?.$el;
      return el instanceof HTMLElement ? el : null;
    }

    const triggerEvents = {
      onMouseenter: (event: MouseEvent) => {
        if (!hoverCapable() || fromTouchTap(event)) {
          return;
        }
        activated.value = true;
        scheduleShow();
      },
      onMouseleave: () => {
        cancelShow();
        if (show.value) {
          hideSoon();
        }
      },
      onFocusin: (event: FocusEvent) => {
        if (
          !keyboardNavigation ||
          !(event.target instanceof Element) ||
          !event.target.matches(":focus-visible")
        ) {
          return;
        }
        const firstActivation = !activated.value;
        activated.value = true;
        scheduleShow();
        if (firstActivation) {
          // Mounting the popover rebuilds the control (the tree gains the
          // tooltip), which drops focus to <body>; hand it back so a keyboard
          // user keeps the tab stop — and the tooltip they just opened.
          restoringFocus = true;
          void nextTick(() => {
            restoringFocus = false;
            if (document.activeElement === document.body) {
              childElement()?.focus({ preventScroll: true });
            }
          });
        }
      },
      onFocusout: () => {
        if (!restoringFocus) {
          hideNow();
        }
      },
      // Hide on press. Not `onClick`: naive's nested-popover path wraps the
      // trigger's own onClick and assumes a single function, so merging ours in
      // would hand it an array. `pointerdown` is not one of the events naive
      // wraps, so it stays a plain listener.
      onPointerdown: () => {
        hideNow();
      },
    };

    /**
     * While a tooltip is open, Escape dismisses it (WCAG 1.4.13 Dismissible).
     * The listener is on the document — not just the trigger — because a HOVER
     * tooltip has no focus on the trigger to receive the key. It never stops
     * propagation, so an enclosing dialog or dropdown still gets the Escape.
     */
    function onEscape(event: KeyboardEvent): void {
      if (event.key === "Escape") {
        hideNow();
      }
    }
    let escapeBound = false;
    watch(show, (visible) => {
      if (typeof document === "undefined" || visible === escapeBound) {
        return;
      }
      escapeBound = visible;
      if (visible) {
        document.addEventListener("keydown", onEscape, true);
      } else {
        document.removeEventListener("keydown", onEscape, true);
      }
    });
    onBeforeUnmount(() => {
      cancelShow();
      cancelHide();
      if (escapeBound) {
        document.removeEventListener("keydown", onEscape, true);
        escapeBound = false;
      }
    });

    const contentEvents = {
      onMouseenter: () => {
        hoveringContent = true;
        cancelHide();
      },
      onMouseleave: () => {
        hoveringContent = false;
        hideNow();
      },
    };

    return (): VNode | VNode[] | null => {
      const node = slots.default?.()[0];
      if (!node) {
        return null;
      }
      // A tooltip that adds copy beyond the accessible name describes the
      // control; one that repeats the name does not (no double announcement).
      const ariaLabel = node.props?.["aria-label"];
      const hasExtraCopy =
        typeof ariaLabel === "string" && ariaLabel !== props.label;
      const trigger = cloneVNode(
        node,
        // NOTE: only plain event handlers are merged. `attrs` is deliberately
        // NOT merged: naive's nested-popover path calls the trigger's handlers
        // assuming single functions, so a caller-supplied handler here would
        // become an array and crash it.
        mergeProps(
          triggerEvents,
          hasExtraCopy ? { "aria-describedby": contentId } : {},
          {
            ref: (el: unknown) => {
              child = el as Element | { $el?: unknown } | null;
            },
          },
        ),
      );
      const tooltip = activated.value
        ? h(
            NTooltip,
            {
              trigger: "manual",
              // No fade: the body mounts/unmounts with `show`, so a TOGGLED
              // tooltip can never linger mid-transition.
              animated: false,
              show: show.value,
              internalSyncTargetWithParent: props.internalSyncTargetWithParent,
              internalInheritedEventHandlers:
                props.internalInheritedEventHandlers,
            },
            {
              trigger: () => trigger,
              default: () =>
                h(
                  "span",
                  {
                    class: "app-tooltip-content",
                    // Purely visual: the description node below carries the
                    // copy for assistive tech (and exists while closed).
                    "aria-hidden": "true",
                    ...contentEvents,
                  },
                  props.label,
                ),
            },
          )
        : trigger;
      return hasExtraCopy
        ? [
            tooltip,
            h(
              "span",
              { id: contentId, role: "tooltip", class: "app-tooltip-sr" },
              props.label,
            ),
          ]
        : tooltip;
    };
  },
});

// naive marks popovers with `__popover__`; without it a wrapping NDropdown would
// bind its own trigger handlers to this wrapper vnode and sync its target
// through it instead of the real trigger button.
(AppTooltip as unknown as { __popover__: boolean }).__popover__ = true;

export default AppTooltip;
</script>
