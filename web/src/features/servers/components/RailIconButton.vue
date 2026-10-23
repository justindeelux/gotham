<script setup lang="ts">
import { NTooltip } from "naive-ui";
import { RouterLink } from "vue-router";
import type { RouteLocationRaw } from "vue-router";

interface Props {
  to: RouteLocationRaw;
  label: string;
  tip?: string;
  linkClass?: string;
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
