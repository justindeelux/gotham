<script setup lang="ts">
import { NAlert, NButton, NModal } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import { installNodeHelper } from "@/features/servers/api/host";
import type { NodeHostState } from "@/features/servers/api/host";
import { errorLog, errorText } from "@/shared/api/errors";

const props = defineProps<{ serverId: string; state: NodeHostState }>();
const emit = defineEmits<{ saved: [state: NodeHostState] }>();
const { t } = useI18n();

const reason = computed<string>(() => {
  const capability = props.state.capability;
  if (capability === "helper_missing" && !props.state.repair) {
    return t("servers.settings.host.capability.helperMissingRemote");
  }
  return capability === "ready" ? "" : t(`servers.settings.host.capability.${capability}`);
});

const installable = computed<boolean>(
  () => props.state.capability === "helper_missing" && props.state.repair,
);

const installing = ref(false);
const installLog = ref("");
const showLog = ref(false);
const errorMessage = ref("");

async function handleInstall(): Promise<void> {
  if (installing.value) {
    return;
  }
  installing.value = true;
  installLog.value = "";
  errorMessage.value = "";
  try {
    const result = await installNodeHelper(props.serverId);
    emit("saved", result.state);
  } catch (error) {
    errorMessage.value = errorText(error) || t("servers.settings.host.helper.installFailed");
    const log = errorLog(error);
    if (log !== "") {
      installLog.value = log;
      showLog.value = true;
    }
  } finally {
    installing.value = false;
  }
}
</script>

<template>
  <div v-if="state.capability !== 'ready'" class="host-helper-gate">
    <NAlert
      :type="installable ? 'warning' : 'info'"
      :show-icon="true"
      class="form-alert"
    >
      {{ reason }}
    </NAlert>
    <div v-if="installable" class="form-alert">
      <NButton type="primary" :loading="installing" @click="handleInstall">
        {{ t("servers.settings.host.helper.install") }}
      </NButton>
    </div>
    <NAlert v-if="errorMessage" type="error" :show-icon="true" class="form-alert">
      {{ errorMessage }}
    </NAlert>

    <NModal
      v-model:show="showLog"
      preset="card"
      :title="t('servers.settings.host.helper.logTitleFailed')"
      class="app-modal"
      style="width: 720px; max-width: 96vw"
    >
      <p class="field-hint">{{ t("servers.settings.host.helper.logHint") }}</p>
      <pre class="host-helper-log">{{ installLog }}</pre>
      <template #footer>
        <NButton @click="showLog = false">
          {{ t("servers.settings.host.helper.close") }}
        </NButton>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.host-helper-log {
  max-height: 40vh;
  overflow: auto;
  margin: 0;
  padding: var(--space-2);
  background: var(--surface-warm);
  color: var(--fg-2);
  border-radius: var(--radius-sm);
  font-size: var(--text-xs);
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
