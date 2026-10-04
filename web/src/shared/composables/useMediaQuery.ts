/**
 * useMediaQuery tracks a CSS media query reactively.
 *
 * Thin re-export of useMediaQuery from @vueuse/core (JUS-23 G1): the local
 * matchMedia wrapper was a 1:1 duplicate. Same call signature, same boolean
 * semantics; importers keep this path so no call site changes.
 */
export { useMediaQuery } from "@vueuse/core";
