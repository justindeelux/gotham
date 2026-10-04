<script setup lang="ts">
import { NTooltip } from "naive-ui";
import { RouterLink } from "vue-router";
import type { RouteLocationRaw } from "vue-router";

interface Props {
  to: RouteLocationRaw;
  /** Accessible name of the link; also the tooltip when `tip` is unset. */
  label: string;
  /** Tooltip copy; defaults to the accessible name. */
  tip?: string;
  /** Extra class for the link (e.g. rail-btn--add). */
  linkClass?: string;
  /** Alert count badge; hidden when unset. Values above 9 render as "9+". */
  badgeCount?: number;
}

const props = defineProps<Props>();
</script>

<template>
  <NTooltip placement="right" trigger="hover">
    <template #trigger>
      <RouterLink
        class="rail-btn"
        :class="props.linkClass"
        :to="props.to"
        :aria-label="props.label"
      >
        <slot />
        <span
          v-if="(props.badgeCount ?? 0) > 0"
          class="rail-badge"
          aria-hidden="true"
        >{{ (props.badgeCount ?? 0) > 9 ? "9+" : props.badgeCount }}</span
        >
      </RouterLink>
    </template>
    <span>{{ props.tip ?? props.label }}</span>
  </NTooltip>
</template>
