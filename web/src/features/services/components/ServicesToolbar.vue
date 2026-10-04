<script setup lang="ts">
import { NInput } from "naive-ui";

import { useServicesPageContext } from "@/features/services/composables/useServicesList";

/** ServicesToolbar renders the status chips and the free-text search. */
const { filters, counts, statusFilter, search } = useServicesPageContext();
</script>

<template>
  <div class="toolbar">
    <div class="chips" role="group" aria-label="Filter services by status">
      <button
        v-for="filter in filters"
        :key="filter.key"
        type="button"
        class="chip"
        :class="{ 'is-active': statusFilter === filter.key }"
        :aria-pressed="statusFilter === filter.key"
        @click="statusFilter = filter.key"
      >
        {{ filter.label }}
        <span class="chip-count">{{ counts[filter.key] }}</span>
      </button>
    </div>
    <NInput
      v-model:value="search"
      clearable
      placeholder="Search service, node or domain…"
      aria-label="Search services"
      class="toolbar__search"
    />
  </div>
</template>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  margin: var(--space-4) 0;
}

.toolbar__search {
  margin-left: auto;
  max-width: 300px;
}

.chips {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: var(--radius-pill);
  border: 1px solid var(--border-soft);
  background: var(--surface);
  color: var(--muted);
  font: inherit;
  font-size: var(--text-xs);
  cursor: pointer;
}

.chip:hover {
  color: var(--fg-2);
}

.chip.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
  border-color: var(--accent);
}

.chip-count {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
}
</style>
