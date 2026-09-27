import { onBeforeUnmount, onMounted, ref } from "vue";
import type { Ref } from "vue";

/** useMediaQuery tracks a CSS media query reactively.
 *
 * Initialized from the current match state on mount so server-rendered or
 * non-browser contexts safely default to false. Used for small responsive
 * adjustments (drawer widths, description columns) without a resize bus.
 */
export function useMediaQuery(query: string): Ref<boolean> {
  const matches = ref(false);
  let media: MediaQueryList | null = null;

  const onChange = (event: MediaQueryListEvent): void => {
    matches.value = event.matches;
  };

  onMounted(() => {
    if (typeof window === "undefined" || !window.matchMedia) {
      return;
    }
    media = window.matchMedia(query);
    matches.value = media.matches;
    media.addEventListener("change", onChange);
  });

  onBeforeUnmount(() => {
    media?.removeEventListener("change", onChange);
    media = null;
  });

  return matches;
}
