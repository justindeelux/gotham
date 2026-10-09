<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  deployChannel,
  getDeploymentBuildLog,
  isActiveDeployment,
} from "@/features/applications/api/applications";
import type { Deployment } from "@/features/applications/api/applications";
import LogViewer from "@/features/servers/components/LogViewer.vue";

/**
 * Build/deploy log for one deployment.
 *
 * An in-flight deployment streams its realtime channel (`LogViewer`); a
 * finished one reads the persisted build log (`GET .../logs`, JUS-84), so a
 * failed run stays inspectable after its Redis stream is gone. The control
 * plane publishes agent output on `logs:{serverID}:{deploymentID}` (see
 * `DeployChannel` in `internal/deploy/events.go`). The streamed output
 * itself is never translated; only the generated heading is.
 */

interface Props {
  /** Node the application is deployed to (server_id of the application). */
  serverId: string;
  /** Deployment whose logs are shown. */
  deployment: Deployment | null;
}

const props = defineProps<Props>();

const { t } = useI18n();

/** Stored log of a finished deployment (null while streaming or unloaded). */
const storedLog = ref<string | null>(null);

const streaming = computed<boolean>(
  () => props.deployment !== null && isActiveDeployment(props.deployment),
);

const channel = computed<string>(() => {
  if (!props.deployment) {
    return "";
  }
  return deployChannel(props.serverId, props.deployment.id);
});

const title = computed<string>(() =>
  props.deployment
    ? String(t("applications.deployLogs.title", { id: props.deployment.id.slice(0, 8) }))
    : String(t("applications.deployLogs.buildLog")),
);

const subtitle = computed<string>(() =>
  props.deployment
    ? `${props.deployment.kind} · ${String(t(`applications.status.${props.deployment.state}`))}`
    : "",
);

/** Loads the stored log whenever a finished deployment is selected. */
watch(
  () => (props.deployment ? `${props.deployment.id}:${props.deployment.state}` : ""),
  async () => {
    const selected = props.deployment;
    storedLog.value = null;
    if (!selected || isActiveDeployment(selected)) {
      return;
    }
    let log: string;
    try {
      // A deployment that finished before the log existed (or whose log never
      // persisted) answers with an empty string, rendered as storedEmpty.
      log = await getDeploymentBuildLog(selected.application_id, selected.id);
    } catch {
      log = "";
    }
    // A newer selection wins: a slow read must not overwrite it.
    if (props.deployment?.id === selected.id) {
      storedLog.value = log;
    }
  },
  { immediate: true },
);
</script>

<template>
  <LogViewer
    v-if="deployment && serverId && streaming"
    :key="channel"
    :server-id="serverId"
    :container-id="deployment.id"
    :channel="channel"
    :title="title"
    :subtitle="subtitle"
  />
  <pre
    v-else-if="deployment && serverId && storedLog !== null"
    class="deploy-logs__stored"
    role="log"
    tabindex="0"
    :aria-label="title"
  >{{ storedLog || t("applications.deployLogs.storedEmpty") }}</pre>
  <p v-else class="deploy-logs__empty">{{ t("applications.deployLogs.empty") }}</p>
</template>

<style scoped>
.deploy-logs__empty {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-sm);
}

.deploy-logs__stored {
  margin: 0;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  line-height: 1.55;
  padding: var(--space-3);
  overflow-y: auto;
  overflow-x: auto;
  min-height: 320px;
  max-height: 56vh;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--fg);
}
</style>
