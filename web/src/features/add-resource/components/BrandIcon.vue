<script setup lang="ts">
import { computed } from "vue";
import type { CSSProperties } from "vue";

import type { BrandIconSize } from "../utils/brands";

interface Props {
  letters: string;
  color: string;
  logo?: string | null;
  size?: BrandIconSize;
}

const props = withDefaults(defineProps<Props>(), {
  logo: null,
  size: "md",
});

const logoUrls = import.meta.glob<string>("../assets/brands/*.svg", {
  eager: true,
  query: "?url",
  import: "default",
});

const logoUrl = computed<string | null>(() =>
  props.logo === null ? null : (logoUrls[`../assets/brands/${props.logo}.svg`] ?? null),
);

const lettersClass = computed<string>(
  () => `brand-icon__letters--${props.letters.length <= 1 ? "single" : "double"}`,
);

const glyphStyle = computed<CSSProperties>(
  () =>
    ({
      "--brand-icon-logo": `url(${JSON.stringify(logoUrl.value)})`,
    }) as CSSProperties,
);
</script>

<template>
  <span
    class="brand-icon"
    :class="`brand-icon--${size}`"
    :style="{ backgroundColor: color }"
    aria-hidden="true"
  >
    <span v-if="logoUrl" class="brand-icon__glyph" :style="glyphStyle" />
    <span v-else class="brand-icon__letters" :class="lettersClass">{{ letters }}</span>
  </span>
</template>

<style scoped>
.brand-icon {
  display: grid;
  place-items: center;
  width: var(--brand-icon-size);
  height: var(--brand-icon-size);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  flex: 0 0 auto;
}

.brand-icon--sm {
  --brand-icon-size: var(--brand-icon-sm);
}

.brand-icon--md {
  --brand-icon-size: var(--brand-icon-md);
}

.brand-icon--lg {
  --brand-icon-size: var(--brand-icon-lg);
}

.brand-icon__glyph {
  width: 62%;
  height: 62%;
  background-color: var(--accent-on);
  mask-image: var(--brand-icon-logo);
  mask-position: center;
  mask-repeat: no-repeat;
  mask-size: contain;
}

.brand-icon__letters {
  font-weight: 700;
  line-height: 1;
  color: var(--accent-on);
  font-family: inherit;
}

.brand-icon__letters--single {
  font-size: calc(var(--brand-icon-size) * 0.4);
}

.brand-icon__letters--double {
  font-size: calc(var(--brand-icon-size) * 0.35);
}
</style>
