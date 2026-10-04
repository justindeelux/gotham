<script setup lang="ts">
import type { DatabaseFilter } from "@/features/databases/utils/databaseFilters";

interface Props {
  counts: Record<DatabaseFilter, number>;
  breakdown: string;
}

defineProps<Props>();
</script>

<template>
  <!-- KPI row ported from databases.html. Size/backup/schedule tiles need
       backend aggregates the API does not expose yet, so this row reports
       the same population the list does. -->
  <div class="kpi-row">
    <div class="stat">
      <p class="stat-label">Databases</p>
      <p class="stat-value num">{{ counts.all }}</p>
      <p class="stat-sub muted">{{ breakdown }}</p>
    </div>
    <div class="stat">
      <p class="stat-label">Running</p>
      <p class="stat-value num">{{ counts.running }}</p>
      <p class="stat-sub muted">of {{ counts.all }}</p>
    </div>
    <div class="stat">
      <p class="stat-label">Public port</p>
      <p class="stat-value num">{{ counts.public }}</p>
      <p class="stat-sub muted">reachable outside the node</p>
    </div>
    <div class="stat">
      <p class="stat-label">Stopped</p>
      <p class="stat-value num">{{ counts.stopped }}</p>
      <p class="stat-sub muted">container stopped, volume intact</p>
    </div>
  </div>
</template>

<style scoped>
.kpi-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-4);
}

.stat {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-4);
}

.stat-label {
  margin: 0 0 var(--space-2);
  font-size: var(--text-xs);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--muted);
}

.stat-value {
  margin: 0 0 var(--space-2);
  font-size: var(--text-3xl);
  color: var(--fg-2);
}

.stat-sub {
  margin: 0;
  font-size: var(--text-xs);
}

@media (max-width: 1180px) {
  .kpi-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .kpi-row {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
