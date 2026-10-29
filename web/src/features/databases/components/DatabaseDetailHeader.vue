<script setup lang="ts">
import { NAvatar } from "naive-ui";
import { inject } from "vue";

import DatabaseStatusTag from "@/features/databases/components/DatabaseStatusTag.vue";
import { databaseDetailKey } from "@/features/databases/composables/useDatabaseDetail";
import ResourceActionButton from "@/features/projects/components/ResourceActionButton.vue";
import ResourceHeader from "@/features/projects/components/ResourceHeader.vue";

import { i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const detail = inject(databaseDetailKey)!;
</script>

<template>
  <ResourceHeader
    :title="detail.database.value?.name ?? detail.shortId.value"
    :resource-id="detail.dbId.value"
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
      <ResourceActionButton
        type="primary"
        icon="refresh"
        :label="t('databases.detail.actions.restart')"
        :loading="detail.databasesStore.acting"
        :disabled="!detail.canRestart.value"
        :disabled-reason="detail.controlHint.value"
        @click="() => void detail.handleLifecycle('restart')"
      />
      <ResourceActionButton
        type="error"
        icon="stop"
        :label="t('databases.detail.actions.stop')"
        :loading="detail.databasesStore.acting"
        :disabled="!detail.canStop.value"
        :disabled-reason="detail.controlHint.value"
        @click="() => void detail.handleLifecycle('stop')"
      />
      <ResourceActionButton
        type="success"
        icon="play"
        :label="t('databases.detail.actions.start')"
        :loading="detail.databasesStore.acting"
        :disabled="!detail.canStart.value"
        :disabled-reason="detail.controlHint.value"
        @click="() => void detail.handleLifecycle('start')"
      />
    </template>
  </ResourceHeader>
</template>
