<script setup lang="ts">
import { NCard, NEmpty, NText } from "naive-ui";
import { useI18n } from "vue-i18n";

import type { Deployment } from "@/features/applications/api/applications";
import DeploymentHistoryTable from "@/features/applications/components/DeploymentHistoryTable.vue";

interface Props {
  deployments: Deployment[];
  loading: boolean;
  /** isCompose switches the footer: a compose rollback re-applies the file. */
  isCompose?: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "show-logs": [deploymentId: string];
  "open-rollback": [deploymentId: string];
}>();

const { t } = useI18n();
</script>

<template>
  <NCard style="margin-top: 16px">
    <DeploymentHistoryTable
      v-if="props.deployments.length > 0 || props.loading"
      :deployments="props.deployments"
      :loading="props.loading"
      paginated
      @show-logs="emit('show-logs', $event)"
      @open-rollback="emit('open-rollback', $event)"
    />
    <NEmpty v-else :description="t('applications.deploymentsTab.noneRecorded')" />
    <template #footer>
      <NText depth="3">
        {{ t(props.isCompose ? "applications.deploymentsTab.footerCompose" : "applications.deploymentsTab.footer") }}
      </NText>
    </template>
  </NCard>
</template>
