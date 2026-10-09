<script setup lang="ts">
import { NCard, NEmpty, NSelect, NSpace, NTabPane, NTabs, NText } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n, I18nT } from "vue-i18n";

import type { Application, Deployment } from "@/features/applications/api/applications";
import DeployLogs from "@/features/applications/components/DeployLogs.vue";
import DeploymentCommitCard from "@/features/applications/components/DeploymentCommitCard.vue";
import LogViewer from "@/features/servers/components/LogViewer.vue";

interface Props {
  logServerId: string;
  logDeploymentId: string;
  serverOptions: Array<{ label: string; value: string }>;
  deploymentOptions: Array<{ label: string; value: string }>;
  activeDeploymentId: string;
  logTarget: Deployment | null;
  effectiveLogServerId: string;
  application: Application | null;
  runtimeDeployment: Deployment | null;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:logServerId": [value: string];
  "update:logDeploymentId": [value: string];
}>();

const { t } = useI18n();

const logTab = ref("deployment");

/** deploymentPlaceholder names the in-flight deployment, if any. */
const deploymentPlaceholder = computed<string>(() =>
  props.activeDeploymentId
    ? String(t("applications.logsTab.streaming", { id: props.activeDeploymentId.slice(0, 8) }))
    : String(t("applications.logsTab.selectDeployment")),
);

/** runtimeContainerId streams the running deployment's container, if any. */
const runtimeContainerId = computed<string>(() => props.runtimeDeployment?.container_id ?? "");

const runtimeTitle = computed<string>(() =>
  props.runtimeDeployment
    ? String(t("applications.logsTab.streaming", { id: props.runtimeDeployment.id.slice(0, 8) }))
    : "",
);
</script>

<template>
  <NCard style="margin-top: 16px">
    <NTabs v-model:value="logTab" type="line" animated>
      <NTabPane name="deployment" :tab="t('applications.logsTab.deployment')">
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
          <DeploymentCommitCard
            :deployment="props.logTarget"
            :repo="props.application?.repo ?? ''"
            :clone-url="props.application?.clone_url ?? ''"
          />
          <NText depth="3">
            <i18n-t keypath="applications.logsTab.hint" tag="span">
              <template #channel><span class="mono">logs:{node}:{deployment}</span></template>
            </i18n-t>
          </NText>
          <DeployLogs
            :server-id="props.effectiveLogServerId"
            :deployment="props.logTarget"
          />
        </NSpace>
      </NTabPane>
      <NTabPane name="runtime" :tab="t('applications.logsTab.runtime')">
        <NSpace vertical :size="12">
          <NSelect
            :value="props.logServerId"
            :options="props.serverOptions"
            :placeholder="t('applications.logsTab.selectNode')"
            style="width: 260px"
            @update:value="emit('update:logServerId', $event)"
          />
          <LogViewer
            v-if="props.runtimeDeployment && props.effectiveLogServerId && runtimeContainerId"
            :key="runtimeContainerId"
            :server-id="props.effectiveLogServerId"
            :container-id="runtimeContainerId"
            :auto-start-stream="true"
            :title="runtimeTitle"
            :subtitle="runtimeContainerId"
          />
          <NEmpty v-else :description="t('applications.logsTab.runtimeEmpty')" />
        </NSpace>
      </NTabPane>
    </NTabs>
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
