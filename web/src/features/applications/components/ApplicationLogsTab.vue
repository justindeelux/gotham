<script setup lang="ts">
import { NCard, NSelect, NSpace, NText } from "naive-ui";

import type { Deployment } from "@/features/applications/api/applications";
import DeployLogs from "@/features/applications/components/DeployLogs.vue";

interface Props {
  logServerId: string;
  logDeploymentId: string;
  serverOptions: Array<{ label: string; value: string }>;
  deploymentOptions: Array<{ label: string; value: string }>;
  activeDeploymentId: string;
  logTarget: Deployment | null;
  effectiveLogServerId: string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:logServerId": [value: string];
  "update:logDeploymentId": [value: string];
}>();
</script>

<template>
  <NCard style="margin-top: 16px">
    <NSpace vertical :size="12">
      <NSpace :size="12">
        <NSelect
          :value="props.logServerId"
          :options="props.serverOptions"
          placeholder="Select node"
          style="width: 260px"
          @update:value="emit('update:logServerId', $event)"
        />
        <NSelect
          :value="props.logDeploymentId"
          :options="props.deploymentOptions"
          :placeholder="
            props.activeDeploymentId
              ? `Streaming: ${props.activeDeploymentId.slice(0, 8)}`
              : 'Select deployment'
          "
          style="width: 280px"
          @update:value="emit('update:logDeploymentId', $event)"
        />
      </NSpace>
      <NText depth="3">
        Logs default to the application's node and fall back to the
        first known one — they stream on
        <span class="mono">logs:{node}:{deployment}</span>.
      </NText>
      <DeployLogs
        :server-id="props.effectiveLogServerId"
        :deployment="props.logTarget"
      />
    </NSpace>
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
