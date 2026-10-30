<script setup lang="ts">
import { computed } from "vue";

import { usageSegments } from "@/features/servers/utils/serverDetailView";

interface Props {
  label: string;
  value: number | null;
  stale?: boolean;
}

const props = defineProps<Props>();
const view = computed(() => usageSegments(props.value));
</script>

<template>
  <div class="usage-row" :class="{ 'is-stale': stale }">
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
      <span v-for="index in 10" :key="index" class="usage-block" />
    </div>
  </div>
</template>

<style scoped>
.usage-row {
  display: grid;
  gap: var(--space-2);
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
  display: grid;
  grid-template-columns: repeat(10, minmax(0, 1fr));
  gap: var(--space-1);
}

.usage-block {
  height: 10px;
  border-radius: var(--radius-sm);
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
