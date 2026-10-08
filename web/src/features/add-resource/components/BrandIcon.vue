<script setup lang="ts">
import { computed } from "vue";

interface Props {
  letters: string;
  color: string;
  label: string;
}

const props = defineProps<Props>();

/** fontSize shrinks the mark text so three glyphs still fit the square. */
const fontSize = computed<number>(() => {
  if (props.letters.length <= 1) {
    return 16;
  }
  if (props.letters.length === 2) {
    return 14;
  }
  return 11;
});
</script>

<template>
  <svg
    class="brand-icon"
    viewBox="0 0 40 40"
    role="img"
    :aria-label="label"
  >
    <rect width="40" height="40" rx="9" :fill="color" />
    <text
      x="20"
      y="26"
      text-anchor="middle"
      :font-size="fontSize"
      font-weight="700"
      fill="#ffffff"
      font-family="inherit"
    >{{ letters }}</text>
  </svg>
</template>

<style scoped>
.brand-icon {
  width: 40px;
  height: 40px;
  border-radius: var(--radius-md);
  flex: 0 0 auto;
}
</style>
