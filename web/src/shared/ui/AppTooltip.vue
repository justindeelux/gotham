<script lang="ts">
import { NTooltip } from "naive-ui";
import { cloneVNode, defineComponent, h, mergeProps, onBeforeUnmount, ref, watch } from "vue";
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
 * events. The event policy is deliberate (JUS-156 review round 1):
 *
 *   · hover      — a real pointer device only ((hover: hover)), so a touch tap's
 *                  synthetic mouseenter never opens a stuck tooltip (F4);
 *   · focus      — a navigation key (Tab / arrow) moved focus here and the
 *                  control matches :focus-visible, so a programmatic focus return
 *                  (a dialog or dropdown handing focus back) does not pop it (F4);
 *   · Escape     — dismisses an open tooltip without swallowing the key, so an
 *                  enclosing dialog/dropdown still receives it (F3, WCAG 1.4.13);
 *   · pointer    — the tooltip stays open while the pointer crosses onto it and
 *                  closes when it leaves either the control or the tooltip (F3).
 *
 * The tooltip content carries role="tooltip" and is exposed through
 * aria-describedby on the control ONLY when the tooltip text differs from the
 * control's aria-label (a disabled-reason hint), so assistive tech reads the
 * extra copy without a double announcement of the name (F5).
 *
 * It keeps naive's `__popover__` contract and forwards the two internal props a
 * wrapping NDropdown sets. naive uses that flag to route the dropdown's own
 * trigger handlers through `internalInheritedEventHandlers` and to sync the
 * dropdown's target through this nested popover, so a dropdown's trigger button
 * carries its tooltip without breaking the dropdown's open/close or focus return.
 */

/** HOVER_GAP_MS leaves a short window for the pointer to reach the tooltip. */
const HOVER_GAP_MS = 120;

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
    () => {
      keyboardNavigation = false;
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
  setup(props, { slots, attrs }) {
    bindInputModality();
    const show = ref(false);
    const contentId = `app-tooltip-${(tooltipSeq += 1)}`;
    let hoveringContent = false;
    let hideTimer: ReturnType<typeof setTimeout> | null = null;

    function cancelHide(): void {
      if (hideTimer !== null) {
        clearTimeout(hideTimer);
        hideTimer = null;
      }
    }

    function hideNow(): void {
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

    /** hoverCapable is false on a touch-only device (also gated in CSS). */
    function hoverCapable(): boolean {
      return (
        typeof window !== "undefined" &&
        window.matchMedia("(hover: hover)").matches
      );
    }

    const triggerEvents = {
      onMouseenter: () => {
        if (!hoverCapable()) {
          return;
        }
        cancelHide();
        show.value = true;
      },
      onMouseleave: () => {
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
        cancelHide();
        show.value = true;
      },
      onFocusout: () => {
        hideNow();
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

    return (): VNode | null => {
      const node = slots.default?.()[0];
      if (!node) {
        return null;
      }
      // Only describe the control when the tooltip adds copy beyond its name.
      const ariaLabel = node.props?.["aria-label"];
      const described =
        typeof ariaLabel === "string" && ariaLabel !== props.label
          ? { "aria-describedby": contentId }
          : {};
      return h(
        NTooltip,
        {
          trigger: "manual",
          // No fade: the body mounts/unmounts with `show`, so a TOGGLED tooltip
          // can never linger mid-transition and no hidden body is left behind.
          animated: false,
          show: show.value,
          internalSyncTargetWithParent: props.internalSyncTargetWithParent,
          internalInheritedEventHandlers: props.internalInheritedEventHandlers,
        },
        {
          trigger: () =>
            cloneVNode(
              node,
              mergeProps(attrs, triggerEvents, described),
            ),
          default: () =>
            h(
              "span",
              {
                id: contentId,
                role: "tooltip",
                class: "app-tooltip-content",
                ...contentEvents,
              },
              props.label,
            ),
        },
      );
    };
  },
});

// naive marks popovers with `__popover__`; without it a wrapping NDropdown would
// bind its own trigger handlers to this wrapper vnode and sync its target
// through it instead of the real trigger button.
(AppTooltip as unknown as { __popover__: boolean }).__popover__ = true;

export default AppTooltip;
</script>
