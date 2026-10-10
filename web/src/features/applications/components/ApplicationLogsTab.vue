<script setup lang="ts">
import { NCard, NEmpty, NSelect, NSpace, NTabPane, NTabs, NText } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

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

/**
 * runtimeServerId streams from the application node, never the shared Logs
 * pick: the container runs on the application's own node.
 */
const runtimeServerId = computed<string>(
  () => props.application?.server_id || props.effectiveLogServerId,
);

/** runtimeNodeMismatch hints when the shared node pick is not the app node. */
const runtimeNodeMismatch = computed<boolean>(() =>
  Boolean(
    props.application?.server_id &&
      props.logServerId &&
      props.logServerId !== props.application.server_id,
  ),
);

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
          <div class="deploy-log-head">
            <div class="deploy-log-picks">
              <NSelect
                :value="props.logServerId"
                :options="props.serverOptions"
                :placeholder="t('applications.logsTab.selectNode')"
                :aria-label="t('applications.logsTab.selectNode')"
                @update:value="emit('update:logServerId', $event)"
              />
              <NSelect
                :value="props.logDeploymentId"
                :options="props.deploymentOptions"
                :placeholder="deploymentPlaceholder"
                :aria-label="t('applications.logsTab.deployment')"
                @update:value="emit('update:logDeploymentId', $event)"
              />
            </div>
            <DeploymentCommitCard
              class="deploy-log-commit"
              :deployment="props.logTarget"
              :repo="props.application?.repo ?? ''"
              :clone-url="props.application?.clone_url ?? ''"
            />
          </div>
          <NText depth="3">
            {{ t("applications.logsTab.hint") }}
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
          <NText v-if="runtimeNodeMismatch" depth="3" data-testid="runtime-node-hint">
            {{ t("applications.logsTab.runtimeNodeHint") }}
          </NText>
          <LogViewer
            v-if="logTab === 'runtime' && props.runtimeDeployment && runtimeServerId && runtimeContainerId"
            :key="runtimeContainerId"
            :server-id="runtimeServerId"
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
/* Commit info sits on the same row as the picks; the picks stack
   vertically. Wraps to one column on narrow widths. */
.deploy-log-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.deploy-log-picks {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  flex: 0 1 280px;
  min-width: min(100%, 240px);
}

.deploy-log-commit {
  flex: 1 1 280px;
  min-width: min(100%, 280px);
  margin: 0;
}
</style>

