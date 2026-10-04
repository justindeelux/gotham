<script setup lang="ts">
import { NButton, NEmpty, NIcon } from "naive-ui";

import {
  databaseEmptyDescription,
  databaseEmptyHint,
} from "@/features/databases/api/databases";
import GothamIcon from "@/shared/ui/GothamIcon.vue";

interface Props {
  totalCount: number;
}

interface Emits {
  create: [];
}

defineProps<Props>();
defineEmits<Emits>();
</script>

<template>
  <NEmpty :description="databaseEmptyDescription(totalCount)">
    <template #icon>
      <NIcon>
        <GothamIcon name="db" />
      </NIcon>
    </template>
    <template #extra>
      <p class="empty-hint">
        {{ databaseEmptyHint(totalCount) }}
      </p>
      <NButton type="primary" @click="$emit('create')">
        Create database
      </NButton>
    </template>
  </NEmpty>
</template>

<style scoped>
.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
}
</style>
