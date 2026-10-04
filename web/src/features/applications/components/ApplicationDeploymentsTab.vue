<script setup lang="ts">
import { NCard, NEmpty, NText } from "naive-ui";

import type { Deployment } from "@/features/applications/api/applications";
import DeploymentHistoryTable from "@/features/applications/components/DeploymentHistoryTable.vue";

interface Props {
  deployments: Deployment[];
  loading: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "show-logs": [deploymentId: string];
  "open-rollback": [deploymentId: string];
}>();
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
    <NEmpty v-else description="No deployments recorded for this application." />
    <template #footer>
      <NText depth="3">
        Rollback only switches the image tag — the old image stays in
        the internal registry.
      </NText>
    </template>
  </NCard>
</template>
