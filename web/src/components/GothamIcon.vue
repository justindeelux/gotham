<script setup lang="ts">
import { computed } from "vue";

// Stroke icon set ported from the docs/design sprite
// (docs/design/assets/gotham-ui.js ICONS map, the UI source of truth).
// Chosen over Naive UI icon components because naive-ui ships no icons of
// its own — the alternative would add an @vicons dependency for four glyphs
// while diverging from the mockup stroke style (1.6px, round caps).
export type IconName = "server" | "shield" | "refresh" | "rocket";

interface Props {
  name: IconName;
}

const props = defineProps<Props>();

const paths: Record<IconName, string> = {
  server:
    '<rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01"/>',
  shield:
    '<path d="M12 3l8 3v6c0 5-3.4 8-8 9-4.6-1-8-4-8-9V6l8-3z"/><path d="M9 12l2 2 4-4"/>',
  refresh:
    '<path d="M4 12a8 8 0 0113.7-5.6M20 12a8 8 0 01-13.7 5.6M20 4v4h-4M4 20v-4h4"/>',
  rocket:
    '<path d="M14 4c4 1 6 3 6 3s-2 2-3 6c-1 3.5-4 6.5-7 7l-3-3c.5-3 3.5-6 7-7z"/><path d="M9 15l-4 4M7 11l-3 1 1-3M13 17l-1 3 3-1"/>',
};

const inner = computed<string>(() => paths[props.name]);
</script>

<template>
  <svg
    class="gotham-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    v-html="inner"
  ></svg>
</template>

<style scoped>
.gotham-icon {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
}
</style>
