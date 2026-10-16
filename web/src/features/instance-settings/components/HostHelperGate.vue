<script setup lang="ts">
import { NAlert, NButton, NModal } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  errorText,
  hostHelperErrorLog,
  installHostHelper,
} from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";

/**
 * HostHelperGate renders the one accurate reason a host section is read-only
 * and, when the in-product install can help, the "Install host helper" action
 * with its installer-output modal (JUS-185). It is meant to render only while
 * its section is not editable; the owning form shows nothing once it is.
 */
const props = defineProps<{ state: InstanceState; section: "network" | "system" }>();
const emit = defineEmits<{ saved: [state: InstanceState] }>();
const { t } = useI18n();

/** supported is whether this host can run the section's stack at all. */
const supported = computed<boolean>(() =>
  props.section === "network" ? props.state.host.network_stack : props.state.host.system_stack,
);

/**
 * installable mirrors the host's repair rule: the in-product install can only
 * succeed when the helper file is missing (so the drop-in with the pinned repair
 * verb is still in place) and the root-owned payload exists. A Denied host, or
 * one without the payload, has no privileged path — the honest "update the
 * control plane" message is shown instead of an install that would fail.
 */
const installable = computed<boolean>(
  () => props.state.host.helper === "missing" && props.state.host.repair,
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
    const result = await installHostHelper();
    // Success unlocks the section: the owner re-renders from the new state and
    // removes this gate. The log is only shown for a failure to fix.
    emit("saved", result.settings);
  } catch (error) {
    errorMessage.value = errorText(error) || t("instance-settings.helper.installFailed");
    const log = hostHelperErrorLog(error);
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
  <div class="host-helper-gate">
    <NAlert v-if="!supported" type="info" :show-icon="true" class="form-alert">
      {{ t(`instance-settings.${section}.stackUnsupported`) }}
    </NAlert>
    <template v-else-if="installable">
      <NAlert type="warning" :show-icon="true" class="form-alert">
        {{ t("instance-settings.helper.missing") }}
      </NAlert>
      <div class="form-alert">
        <NButton type="primary" :loading="installing" @click="handleInstall">
          {{ t("instance-settings.helper.install") }}
        </NButton>
      </div>
    </template>
    <NAlert v-else type="info" :show-icon="true" class="form-alert">
      {{ t("instance-settings.helper.restart") }}
    </NAlert>
    <NAlert v-if="errorMessage" type="error" :show-icon="true" class="form-alert">
      {{ errorMessage }}
    </NAlert>

    <NModal
      v-model:show="showLog"
      preset="card"
      :title="t('instance-settings.helper.logTitleFailed')"
      class="app-modal"
      style="width: 720px; max-width: 96vw"
    >
      <p class="field-hint">{{ t("instance-settings.helper.logHint") }}</p>
      <pre class="host-helper-log">{{ installLog }}</pre>
      <template #footer>
        <NButton @click="showLog = false">
          {{ t("instance-settings.helper.close") }}
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
