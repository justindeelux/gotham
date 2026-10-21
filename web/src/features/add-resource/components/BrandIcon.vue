<script setup lang="ts">
import { computed } from "vue";

interface Props {
  /** Letter fallback shown when no vendored glyph matches (or is missing). */
  letters: string;
  /** Avatar background hue; the glyph is always white. */
  color: string;
  /** Vendored Simple Icons slug (see `assets/brands`). */
  logo?: string | null;
  /** Frame edge length in px: 40 picker, 44 detail, 28 environment list. */
  size?: number;
}

const props = withDefaults(defineProps<Props>(), {
  logo: null,
  size: 40,
});

/** logoUrls bundles every vendored glyph; a slug without a file falls back. */
const logoUrls = import.meta.glob<string>("../assets/brands/*.svg", {
  eager: true,
  query: "?url",
  import: "default",
});

const logoUrl = computed<string | null>(() =>
  props.logo === null ? null : (logoUrls[`../assets/brands/${props.logo}.svg`] ?? null),
);

/** fontSize shrinks the mark text so three glyphs still fit the square. */
const fontSize = computed<number>(() => {
  if (props.letters.length <= 1) {
    return props.size * 0.4;
  }
  if (props.letters.length === 2) {
    return props.size * 0.35;
  }
  return props.size * 0.275;
});
</script>

<template>
  <span
    class="brand-icon"
    :style="{ width: `${size}px`, height: `${size}px`, backgroundColor: color }"
    aria-hidden="true"
  >
    <span
      v-if="logoUrl"
      class="brand-icon__glyph"
      :style="{ maskImage: `url(${JSON.stringify(logoUrl)})`, WebkitMaskImage: `url(${JSON.stringify(logoUrl)})` }"
    />
    <span v-else class="brand-icon__letters" :style="{ fontSize: `${fontSize}px` }">{{ letters }}</span>
  </span>
</template>

<style scoped>
.brand-icon {
  display: grid;
  place-items: center;
  border-radius: var(--radius-md);
  flex: 0 0 auto;
}

/* The approved treatment: a white silhouette at ~62% of the colored frame. */
.brand-icon__glyph {
  width: 62%;
  height: 62%;
  background-color: #ffffff;
  mask-position: center;
  mask-size: contain;
  mask-repeat: no-repeat;
}

.brand-icon__letters {
  font-weight: 700;
  line-height: 1;
  color: #ffffff;
  font-family: inherit;
}
</style>
