<script setup lang="ts">
import { NAvatar, NButton, NPopconfirm, NSpace, NText } from "naive-ui";
import { inject } from "vue";

import DatabaseStatusTag from "@/features/databases/components/DatabaseStatusTag.vue";
import { databaseDetailKey } from "@/features/databases/composables/useDatabaseDetail";

const detail = inject(databaseDetailKey)!;
</script>

<template>
  <div class="page-head">
    <NAvatar round :size="48">{{ detail.initials.value }}</NAvatar>
    <div class="page-head__title">
      <NSpace align="center" :size="10">
        <NText strong style="font-size: 20px" class="mono">
          {{ detail.database.value?.name ?? detail.shortId.value }}
        </NText>
        <DatabaseStatusTag
          v-if="detail.database.value"
          :status="detail.database.value.status"
          size="medium"
        />
      </NSpace>
      <NText depth="3" class="mono">{{ detail.dbId.value }}</NText>
    </div>
    <NSpace class="page-head__actions" align="center" :size="8">
      <NButton
        :disabled="!detail.canStart.value"
        :loading="detail.databasesStore.acting"
        @click="() => void detail.handleLifecycle('start')"
      >
        Start
      </NButton>
      <NButton
        :disabled="!detail.canStop.value"
        :loading="detail.databasesStore.acting"
        @click="() => void detail.handleLifecycle('stop')"
      >
        Stop
      </NButton>
      <NButton
        :disabled="!detail.canRestart.value"
        :loading="detail.databasesStore.acting"
        @click="() => void detail.handleLifecycle('restart')"
      >
        Restart
      </NButton>
      <NButton :disabled="!detail.database.value" @click="detail.openRename">
        Rename
      </NButton>
      <NPopconfirm @positive-click="() => void detail.handleDelete()">
        <template #trigger>
          <NButton type="error" ghost :loading="detail.databasesStore.acting">
            Delete
          </NButton>
        </template>
        Delete this database? The container is removed from the node, the
        volume {{ detail.database.value?.volume ?? "" }} is kept for 7 days before
        permanent removal.
      </NPopconfirm>
    </NSpace>
  </div>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.page-head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.page-head__title {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  flex: 1;
  min-width: 0;
}

.page-head__actions {
  margin-left: auto;
  flex-shrink: 0;
}
</style>
