<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { CSSProperties } from "vue";

import { assetFor } from "../utils/brandAssets";
import type { BrandAsset } from "../utils/brandAssets";
import type { BrandIconSize } from "../utils/brands";

interface Props {
  letters: string;
  color: string;
  logo?: string | null;
  glyph?: boolean;
  size?: BrandIconSize;
}

const props = withDefaults(defineProps<Props>(), {
  logo: null,
  glyph: false,
  size: "md",
});

const asset = ref<BrandAsset | null>(null);

watch(
  () => [props.logo, props.glyph] as const,
  async ([logo, glyph]) => {
    const next = logo === null ? null : await assetFor(logo, glyph);
    if (props.logo === logo && props.glyph === glyph) {
      asset.value = next;
    }
  },
  { immediate: true },
);

const imageUrl = computed<string | null>(() =>
  asset.value === null || asset.value.tinted ? null : asset.value.url,
);

const glyphUrl = computed<string | null>(() =>
  asset.value !== null && asset.value.tinted ? asset.value.url : null,
);

const frameStyle = computed<CSSProperties>(() =>
  imageUrl.value === null ? { backgroundColor: props.color } : {},
);

const glyphStyle = computed<CSSProperties>(
  () =>
    ({
      "--brand-icon-logo": `url(${JSON.stringify(glyphUrl.value)})`,
    }) as CSSProperties,
);

const lettersClass = computed<string>(
  () => `brand-icon__letters--${props.letters.length <= 1 ? "single" : "double"}`,
);
</script>

<template>
  <span
    class="brand-icon"
    :class="[`brand-icon--${size}`, { 'brand-icon--image': imageUrl !== null }]"
    :style="frameStyle"
    aria-hidden="true"
  >
    <img v-if="imageUrl !== null" class="brand-icon__image" :src="imageUrl" alt="" />
    <span v-else-if="glyphUrl !== null" class="brand-icon__glyph" :style="glyphStyle" />
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

.brand-icon--image {
  background-color: var(--surface-warm);
}

.brand-icon__image {
  width: 76%;
  height: 76%;
  object-fit: contain;
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
