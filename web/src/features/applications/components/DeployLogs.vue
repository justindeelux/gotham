<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { deployChannel } from "@/features/applications/api/applications";
import type { Deployment } from "@/features/applications/api/applications";
import LogViewer from "@/features/servers/components/LogViewer.vue";

/**
 * Realtime build/deploy log for one deployment.
 *
 * The control plane publishes agent output on `logs:{serverID}:{deploymentID}`
 * (see `DeployChannel` in `internal/deploy/events.go`), so this wrapper only
 * resolves the channel and reuses the Phase 3 `LogViewer` unchanged — log
 * rendering, pause/follow/clear/download all come from that component. The
 * streamed output itself is never translated; only the generated heading is.
 */

interface Props {
  /** Node the application is deployed to (server_id of the application). */
  serverId: string;
  /** Deployment whose logs are streamed. */
  deployment: Deployment | null;
}

const props = defineProps<Props>();

const { t } = useI18n();

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
    ? `${props.deployment.kind} · ${props.deployment.state}`
    : "",
);
</script>

<template>
  <LogViewer
    v-if="deployment && serverId"
    :server-id="serverId"
    :container-id="deployment.id"
    :channel="channel"
    :title="title"
    :subtitle="subtitle"
  />
  <p v-else class="deploy-logs__empty">{{ t("applications.deployLogs.empty") }}</p>
</template>

<style scoped>
.deploy-logs__empty {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-sm);
}
</style>
