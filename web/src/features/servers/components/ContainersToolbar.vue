<script setup lang="ts">
import { NIcon, NInput } from "naive-ui";

import GothamIcon from "@/shared/ui/GothamIcon.vue";
import type { ContainerFilter } from "@/features/servers/utils/containerView";

interface Props {
  activeFilter: ContainerFilter;
  filterCounts: Record<ContainerFilter, number>;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:activeFilter": [filter: ContainerFilter];
}>();

const searchQuery = defineModel<string>("searchQuery", { required: true });

function selectFilter(filter: ContainerFilter): void {
  emit("update:activeFilter", filter);
}

function isActive(filter: ContainerFilter): boolean {
  return props.activeFilter === filter;
}
</script>

<template>
  <div class="toolbar">
    <div class="filters" role="group" :aria-label="$t('servers.toolbar.filterContainers')">
      <button
        class="chip"
        type="button"
        :class="{ 'is-active': isActive('all') }"
        :aria-pressed="isActive('all')"
        @click="selectFilter('all')"
      >
        {{ $t("servers.filters.all") }} <span class="nav-count">{{ filterCounts.all }}</span>
      </button>
      <button
        class="chip"
        type="button"
        :class="{ 'is-active': isActive('running') }"
        :aria-pressed="isActive('running')"
        @click="selectFilter('running')"
      >
        {{ $t("servers.filters.running") }} <span class="nav-count">{{ filterCounts.running }}</span>
      </button>
      <button
        class="chip"
        type="button"
        :class="{ 'is-active': isActive('exited') }"
        :aria-pressed="isActive('exited')"
        @click="selectFilter('exited')"
      >
        {{ $t("servers.filters.exited") }} <span class="nav-count">{{ filterCounts.exited }}</span>
      </button>
    </div>
    <NInput
      v-model:value="searchQuery"
      class="search-input"
      :placeholder="$t('servers.toolbar.searchContainersPlaceholder')"
      :aria-label="$t('servers.toolbar.searchContainers')"
      clearable
    >
      <template #prefix>
        <NIcon>
          <GothamIcon name="search" />
        </NIcon>
      </template>
    </NInput>
  </div>
</template>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  margin-bottom: var(--space-4);
}

.filters {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-family: var(--font-body);
  font-size: var(--text-sm);
  color: var(--muted);
  background: transparent;
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 6px 14px;
  cursor: pointer;
  transition: background var(--motion-base) var(--ease-standard),
    color var(--motion-base) var(--ease-standard),
    border-color var(--motion-base) var(--ease-standard);
}

.chip:hover {
  background: var(--hover-row);
  color: var(--fg-2);
  border-color: var(--border-soft);
}

.chip.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
  border-color: var(--border-soft);
}

.chip .nav-count {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
}

.search-input {
  margin-left: auto;
  max-width: 280px;
}
</style>
