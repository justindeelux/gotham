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

/** True when the stored-log read failed (distinct from a genuinely empty log). */
const loadFailed = ref(false);

/** Monotonic read id: a slow read must not overwrite a newer selection. */
let readSeq = 0;

/** Manual retry trigger for the error state's retry button. */
const retryNonce = ref(0);

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

/** True while a finished deployment's stored log is being read. */
const loadingStored = computed<boolean>(
  () => !streaming.value && !!props.deployment && !!props.serverId && storedLog.value === null && !loadFailed.value,
);

/**
 * A deployment counts as just finished when its clock is missing or recent:
 * only then is an empty stored log worth re-reading (the persist may still
 * be landing). Older rows with no log predate log persistence (JUS-84).
 */
function justFinished(selected: Deployment): boolean {
  if (!selected.finished_at) {
    return true;
  }
  const finished = Date.parse(selected.finished_at);
  if (Number.isNaN(finished)) {
    return true;
  }
  return Date.now() - finished < 60_000;
}

/** Loads the stored log whenever a finished deployment is selected. */
watch(
  () => [
    props.deployment ? `${props.deployment.id}:${props.deployment.state}` : "",
    retryNonce.value,
  ],
  async () => {
    const selected = props.deployment;
    const seq = ++readSeq;
    storedLog.value = null;
    loadFailed.value = false;
    if (!selected || isActiveDeployment(selected)) {
      return;
    }
    const fresh = () => seq === readSeq && props.deployment?.id === selected.id;
    // An empty read right after the terminal event can precede the persist;
    // retry briefly before settling on storedEmpty.
    const attempts = justFinished(selected) ? 3 : 1;
    for (let attempt = 0; attempt < attempts; attempt++) {
      if (attempt > 0) {
        await new Promise((resolve) => setTimeout(resolve, 500));
      }
      if (!fresh()) {
        return;
      }
      try {
        const log = await getDeploymentBuildLog(selected.application_id, selected.id);
        if (!fresh()) {
          return;
        }
        if (log !== "" || attempt + 1 >= attempts) {
          storedLog.value = log;
          return;
        }
      } catch {
        if (!fresh()) {
          return;
        }
        loadFailed.value = true;
        return;
      }
    }
  },
  { immediate: true },
);

/** Re-reads the stored log after a failed read. */
function retryLoad(): void {
  retryNonce.value += 1;
}
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
  <p v-else-if="deployment && serverId && loadFailed" class="deploy-logs__error" role="alert">
    {{ t("applications.deployLogs.loadError") }}
    <button type="button" class="deploy-logs__retry" @click="retryLoad">
      {{ t("applications.deployLogs.retry") }}
    </button>
  </p>
  <p v-else-if="loadingStored" class="deploy-logs__empty">
    {{ t("applications.deployLogs.loading") }}
  </p>
  <p v-else class="deploy-logs__empty">{{ t("applications.deployLogs.empty") }}</p>
</template>

<style scoped>
.deploy-logs__empty {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-sm);
}

.deploy-logs__error {
  margin: 0;
  color: var(--danger);
  font-size: var(--text-sm);
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.deploy-logs__retry {
  cursor: pointer;
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
