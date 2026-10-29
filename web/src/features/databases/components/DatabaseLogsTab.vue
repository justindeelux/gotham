<script setup lang="ts">
import { NEmpty, NSpace, NText } from "naive-ui";
import { useI18n } from "vue-i18n";

import type { Database } from "@/features/databases/api/databases";
import LogViewer from "@/features/servers/components/LogViewer.vue";

interface Props {
  database: Database;
  active: boolean;
}

const props = defineProps<Props>();

const { t } = useI18n();
</script>

<template>
  <NSpace vertical :size="12" style="margin-top: 16px">
    <NEmpty
      v-if="!props.database.container_id"
      :description="t('databases.detail.logs.empty')"
    />
    <template v-else>
      <NText v-if="props.database.status === 'stopped'" depth="3">
        {{ t("databases.detail.logs.stopped") }}
      </NText>
      <LogViewer
        :key="props.database.container_id"
        :server-id="props.database.server_id"
        :container-id="props.database.container_id"
        :auto-start-stream="props.active"
        :title="t('databases.detail.logs.title')"
        :subtitle="props.database.container_id"
      />
    </template>
  </NSpace>
</template>
