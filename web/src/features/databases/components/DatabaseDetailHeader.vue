<script setup lang="ts">
import { NAvatar, NButton } from "naive-ui";
import { computed, inject } from "vue";

import { databaseBrand } from "@/features/add-resource";
import DatabaseStatusTag from "@/features/databases/components/DatabaseStatusTag.vue";
import { databaseDetailKey } from "@/features/databases/composables/useDatabaseDetail";
import ResourceHeader from "@/features/projects/components/ResourceHeader.vue";

import { i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const detail = inject(databaseDetailKey)!;

const brand = computed(() => {
  const database = detail.database.value;
  return database ? databaseBrand(database.engine, database.name) : null;
});
</script>

<template>
  <ResourceHeader
    :title="detail.database.value?.name ?? detail.shortId.value"
    :resource-id="detail.dbId.value"
    :brand="brand"
  >
    <template #identity>
      <NAvatar round :size="48">{{ detail.initials.value }}</NAvatar>
    </template>
    <template #badges>
      <DatabaseStatusTag
        v-if="detail.database.value"
        :status="detail.database.value.status"
        size="medium"
      />
    </template>
    <template #actions>
      <NButton
        :disabled="!detail.canStart.value"
        :loading="detail.databasesStore.acting"
        @click="() => void detail.handleLifecycle('start')"
      >
        {{ t("databases.detail.actions.start") }}
      </NButton>
      <NButton
        :disabled="!detail.canStop.value"
        :loading="detail.databasesStore.acting"
        @click="() => void detail.handleLifecycle('stop')"
      >
        {{ t("databases.detail.actions.stop") }}
      </NButton>
      <NButton
        :disabled="!detail.canRestart.value"
        :loading="detail.databasesStore.acting"
        @click="() => void detail.handleLifecycle('restart')"
      >
        {{ t("databases.detail.actions.restart") }}
      </NButton>
    </template>
  </ResourceHeader>
</template>
