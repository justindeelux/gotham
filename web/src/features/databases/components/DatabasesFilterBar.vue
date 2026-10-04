<script setup lang="ts">
import { NButton, NInput, NSpace } from "naive-ui";

import type { DatabaseFilter } from "@/features/databases/utils/databaseFilters";

interface Props {
  activeFilter: DatabaseFilter;
  counts: Record<DatabaseFilter, number>;
  searchQuery: string;
}

interface Emits {
  "update:activeFilter": [value: DatabaseFilter];
  "update:searchQuery": [value: string];
}

defineProps<Props>();
defineEmits<Emits>();
</script>

<template>
  <NSpace :size="8" align="center">
    <NButton
      size="small"
      :secondary="activeFilter !== 'all'"
      :type="activeFilter === 'all' ? 'primary' : undefined"
      round
      @click="$emit('update:activeFilter', 'all')"
    >
      All · {{ counts.all }}
    </NButton>
    <NButton
      size="small"
      :secondary="activeFilter !== 'running'"
      :type="activeFilter === 'running' ? 'primary' : undefined"
      round
      @click="$emit('update:activeFilter', 'running')"
    >
      Running · {{ counts.running }}
    </NButton>
    <NButton
      size="small"
      :secondary="activeFilter !== 'stopped'"
      :type="activeFilter === 'stopped' ? 'primary' : undefined"
      round
      @click="$emit('update:activeFilter', 'stopped')"
    >
      Stopped · {{ counts.stopped }}
    </NButton>
    <NButton
      size="small"
      :secondary="activeFilter !== 'public'"
      :type="activeFilter === 'public' ? 'primary' : undefined"
      round
      @click="$emit('update:activeFilter', 'public')"
    >
      Public port · {{ counts.public }}
    </NButton>
    <NInput
      :value="searchQuery"
      class="search-input"
      placeholder="Search databases…"
      aria-label="Search databases"
      clearable
      @update:value="$emit('update:searchQuery', $event)"
    />
  </NSpace>
</template>

<style scoped>
.search-input {
  margin-left: auto;
  max-width: 280px;
}

@media (max-width: 860px) {
  .search-input {
    margin-left: 0;
    max-width: none;
    width: 100%;
  }
}
</style>
