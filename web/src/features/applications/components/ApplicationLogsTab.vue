<script setup lang="ts">
import { NCard, NSelect, NSpace, NText } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

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

const { t } = useI18n();

/** deploymentPlaceholder names the in-flight deployment, if any. */
const deploymentPlaceholder = computed<string>(() =>
  props.activeDeploymentId
    ? String(t("applications.logsTab.streaming", { id: props.activeDeploymentId.slice(0, 8) }))
    : String(t("applications.logsTab.selectDeployment")),
);
</script>

<template>
  <NCard style="margin-top: 16px">
    <NSpace vertical :size="12">
      <NSpace :size="12">
        <NSelect
          :value="props.logServerId"
          :options="props.serverOptions"
          :placeholder="t('applications.logsTab.selectNode')"
          style="width: 260px"
          @update:value="emit('update:logServerId', $event)"
        />
        <NSelect
          :value="props.logDeploymentId"
          :options="props.deploymentOptions"
          :placeholder="deploymentPlaceholder"
          style="width: 280px"
          @update:value="emit('update:logDeploymentId', $event)"
        />
      </NSpace>
      <NText depth="3">
        {{ t("applications.logsTab.hint") }}
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
