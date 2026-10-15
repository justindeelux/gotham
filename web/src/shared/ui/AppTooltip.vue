<script lang="ts">
import { NTooltip } from "naive-ui";
import { cloneVNode, defineComponent, h, mergeProps, ref } from "vue";
import type { PropType, VNode } from "vue";

/**
 * AppTooltip is the one tooltip behind every icon-only control. It renders no
 * wrapper of its own — the single slotted control is cloned in place — so a
 * button's layout, focus and hit area are untouched.
 *
 * It opens on BOTH pointer hover and keyboard focus. naive's NTooltip alone
 * cannot: its `trigger` is a single value ("hover" xor "focus"), so the tooltip
 * here is driven through naive's controlled `show` (trigger="manual") and the
 * open/close state is wired to the child's own mouseenter/mouseleave and
 * focusin/focusout events. focusin/focusout bubble, so they cover whichever
 * descendant actually holds focus.
 *
 * It keeps naive's `__popover__` contract and forwards the two internal props
 * a wrapping NDropdown sets, so a dropdown's trigger button carries its tooltip
 * without breaking the dropdown's open/close or the focus return.
 */
const AppTooltip = defineComponent({
  name: "AppTooltip",
  inheritAttrs: false,
  props: {
    /** Tooltip copy; the control's own i18n label (same as its aria-label). */
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
    const show = ref(false);
    return (): VNode | null => {
      const node = slots.default?.()[0];
      if (!node) {
        return null;
      }
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
              mergeProps(attrs, {
                onMouseenter: () => {
                  show.value = true;
                },
                onMouseleave: () => {
                  show.value = false;
                },
                onFocusin: () => {
                  show.value = true;
                },
                onFocusout: () => {
                  show.value = false;
                },
              }),
            ),
          default: () => props.label,
        },
      );
    };
  },
});

// naive marks popovers with `__popover__`. Without it a wrapping NDropdown would
// measure this wrapper — a fragment — instead of the real trigger element.
(AppTooltip as unknown as { __popover__: boolean }).__popover__ = true;

export default AppTooltip;
</script>
