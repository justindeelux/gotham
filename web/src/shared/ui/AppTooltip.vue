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

const HOVER_GAP_MS = 120;

const SHOW_DELAY_MS = 500;

const TOUCH_TAP_MS = 600;

const NAVIGATION_KEYS = new Set([
  "Tab",
  "ArrowUp",
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
]);

let keyboardNavigation = false;
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
  document.addEventListener("focusin", () => {
    keyboardNavigation = false;
  });
}

let tooltipSeq = 0;

const AppTooltip = defineComponent({
  name: "AppTooltip",
  inheritAttrs: false,
  props: {
    label: { type: String, required: true },
    internalSyncTargetWithParent: Boolean,
    internalInheritedEventHandlers: {
      type: Array as unknown as PropType<never[]>,
      default: () => [],
    },
  },
  setup(props, { slots }) {
    bindInputModality();
    const show = ref(false);
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
    let restoringFocus = false;

    function cancelHide(): void {
      if (hideTimer !== null) {
        clearTimeout(hideTimer);
        hideTimer = null;
      }
    }

    function cancelShow(): void {
      if (showTimer !== null) {
        clearTimeout(showTimer);
        showTimer = null;
      }
    }

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

    function hideSoon(): void {
      cancelHide();
      hideTimer = setTimeout(() => {
        hideTimer = null;
        if (!hoveringContent) {
          show.value = false;
        }
      }, HOVER_GAP_MS);
    }

    function hoverCapable(): boolean {
      return (
        typeof window !== "undefined" &&
        window.matchMedia("(hover: hover)").matches
      );
    }

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
      onPointerdown: () => {
        hideNow();
      },
    };

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
      const ariaLabel = node.props?.["aria-label"];
      const hasExtraCopy =
        typeof ariaLabel === "string" && ariaLabel !== props.label;
      const trigger = cloneVNode(
        node,
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

(AppTooltip as unknown as { __popover__: boolean }).__popover__ = true;

export default AppTooltip;
</script>
