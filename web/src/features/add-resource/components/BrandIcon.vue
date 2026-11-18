<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { CSSProperties } from "vue";

import type { BrandIconSize } from "../utils/brands";

interface Props {
  letters: string;
  color: string;
  logo?: string | null;
  size?: BrandIconSize;
}

interface BrandAsset {
  url: string;
  tinted: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  logo: null,
  size: "md",
});

// Lazy loaders keep the icons out of the main chunk; the gallery has hundreds.
const iconLoaders = import.meta.glob<string>(
  "../assets/icons/**/*.{svg,png,webp,jpeg,jpg,ico}",
  { query: "?url", import: "default" },
);
const glyphLoaders = import.meta.glob<string>("../assets/brands/*.svg", {
  query: "?url",
  import: "default",
});

type Loader = () => Promise<string>;

function keyed(loaders: Record<string, Loader>): Map<string, Loader> {
  return new Map(
    Object.entries(loaders).map(([path, load]) => [
      path.slice(path.lastIndexOf("/") + 1).replace(/\.[^.]+$/, ""),
      load,
    ]),
  );
}

const icons = keyed(iconLoaders);
const glyphs = keyed(glyphLoaders);
// shortcut: one key namespace for both icon folders, no service/database name collides today
const cache = new Map<string, Promise<BrandAsset>>();

async function loadAsset(logo: string): Promise<BrandAsset> {
  const glyph = glyphs.get(logo);
  if (glyph !== undefined) {
    return { url: await glyph(), tinted: true };
  }
  const icon = icons.get(logo);
  if (icon !== undefined) {
    return { url: await icon(), tinted: false };
  }
  const fallback = icons.get("default");
  return { url: fallback === undefined ? "" : await fallback(), tinted: false };
}

function assetFor(logo: string): Promise<BrandAsset> {
  let pending = cache.get(logo);
  if (pending === undefined) {
    pending = loadAsset(logo);
    cache.set(logo, pending);
  }
  return pending;
}

const asset = ref<BrandAsset | null>(null);

watch(
  () => props.logo,
  async (logo) => {
    const next = logo === null ? null : await assetFor(logo);
    if (props.logo === logo) {
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
