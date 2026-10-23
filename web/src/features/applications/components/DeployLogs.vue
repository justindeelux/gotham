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

interface Props {
  serverId: string;
  deployment: Deployment | null;
}

const props = defineProps<Props>();

const { t } = useI18n();

const storedLog = ref<string | null>(null);

const loadFailed = ref(false);

let readSeq = 0;

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

const loadingStored = computed<boolean>(
  () => !streaming.value && !!props.deployment && !!props.serverId && storedLog.value === null && !loadFailed.value,
);

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

watch(
  [() => (props.deployment ? `${props.deployment.id}:${props.deployment.state}` : ""), retryNonce],
  async () => {
    const selected = props.deployment;
    const seq = ++readSeq;
    storedLog.value = null;
    loadFailed.value = false;
    if (!selected || isActiveDeployment(selected)) {
      return;
    }
    const fresh = () => seq === readSeq && props.deployment?.id === selected.id;
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
