<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";

import {
  usageBlockCount,
  usageBlockCountForWidth,
  usageSegments,
} from "@/features/servers/utils/serverDetailView";

interface Props {
  label: string;
  value: number | null;
  stale?: boolean;
}

const props = defineProps<Props>();

const rowEl = ref<HTMLElement | null>(null);
const blockCount = ref(usageBlockCount);
const view = computed(() => usageSegments(props.value, blockCount.value));

let observer: ResizeObserver | null = null;

function measure(): void {
  blockCount.value = usageBlockCountForWidth(rowEl.value?.clientWidth ?? 0);
}

onMounted(() => {
  measure();
  if (typeof ResizeObserver !== "undefined" && rowEl.value !== null) {
    observer = new ResizeObserver(measure);
    observer.observe(rowEl.value);
  }
});

onBeforeUnmount(() => {
  observer?.disconnect();
  observer = null;
});
</script>

<template>
  <div ref="rowEl" class="usage-row" :class="{ 'is-stale': stale }">
    <div class="usage-head">
      <span class="usage-label">{{ label }}</span>
      <span class="usage-value">{{ view ? `${view.percentage}%` : "—" }}</span>
    </div>
    <div
      v-if="view"
      class="usage-segments"
      role="meter"
      :aria-label="`${label} ${view.percentage}%`"
      :aria-valuenow="view.percentage"
      aria-valuemin="0"
      aria-valuemax="100"
    >
      <span
        v-for="(block, index) in view.blocks"
        :key="index"
        class="usage-block"
        :class="[`is-${block.level}`, { 'is-filled': block.filled }]"
      />
    </div>
    <div v-else class="usage-segments" aria-hidden="true">
      <span v-for="index in blockCount" :key="index" class="usage-block" />
    </div>
  </div>
</template>

<style scoped>
.usage-row {
  display: grid;
  gap: var(--space-2);
  min-width: 0;
}

.usage-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-2);
}

.usage-label {
  color: var(--muted);
  font-size: var(--text-sm);
}

.usage-value {
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  font-variant-numeric: tabular-nums;
}

.usage-segments {
  display: flex;
  gap: calc(var(--space-1) / 2);
  min-width: 0;
}

.usage-block {
  flex: 0 0 auto;
  width: var(--space-1);
  height: var(--space-2);
  background: var(--border-soft);
  transition: background-color var(--motion-base) var(--ease-standard);
}

.usage-block.is-filled.is-success {
  background: var(--success);
}

.usage-block.is-filled.is-warning {
  background: var(--warn);
}

.usage-block.is-filled.is-danger {
  background: var(--danger);
}

.usage-row.is-stale .usage-value,
.usage-row.is-stale .usage-segments {
  opacity: 0.5;
}

@media (prefers-reduced-motion: reduce) {
  .usage-block {
    transition: none;
  }
}
</style>
