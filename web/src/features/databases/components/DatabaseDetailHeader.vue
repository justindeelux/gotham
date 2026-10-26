<script setup lang="ts">
import { NAvatar, NButton, NPopconfirm } from "naive-ui";
import { inject } from "vue";

import DatabaseStatusTag from "@/features/databases/components/DatabaseStatusTag.vue";
import { databaseDetailKey } from "@/features/databases/composables/useDatabaseDetail";
import PageHeader from "@/shared/ui/PageHeader.vue";

import { i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const detail = inject(databaseDetailKey)!;
</script>

<template>
  <PageHeader>
    <template #identity>
      <NAvatar round :size="48">{{ detail.initials.value }}</NAvatar>
    </template>
    <template #title>
      {{ detail.database.value?.name ?? detail.shortId.value }}
    </template>
    <template #badges>
      <DatabaseStatusTag
        v-if="detail.database.value"
        :status="detail.database.value.status"
        size="medium"
      />
    </template>
    <template #meta>{{ detail.dbId.value }}</template>
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
      <NButton :disabled="!detail.database.value" @click="detail.openRename">
        {{ t("databases.detail.actions.rename") }}
      </NButton>
      <NPopconfirm @positive-click="() => void detail.handleDelete()">
        <template #trigger>
          <NButton type="error" ghost :loading="detail.databasesStore.acting">
            {{ t("common.actions.delete") }}
          </NButton>
        </template>
        {{
          t("databases.detail.deleteConfirm", {
            volume: detail.database.value?.volume ?? "",
          })
        }}
      </NPopconfirm>
    </template>
  </PageHeader>
</template>
