<script setup lang="ts">
import { NAlert, NButton, NModal, NText } from "naive-ui";
import { computed, inject } from "vue";

import { WizardKey } from "@/features/servers/composables/useAddServerWizard";
import InstallJobLogPane from "@/features/toolchain/components/InstallJobLogPane.vue";
import type { InstallLogPaneState } from "@/features/toolchain/components/InstallJobLogPane.vue";

/**
 * Streamed log modal for the pre-agent Docker install (JUS-187). It reuses the
 * App Store's log pane: the install runs on the node over the wizard's SSH
 * session, so no agent is required.
 */
const wizard = inject(WizardKey);
if (!wizard) {
  throw new Error("DockerInstallModal must be used inside AddServerWizard.");
}

const show = computed<boolean>({
  get: () => wizard.dockerInstallOpen.value,
  set: (value: boolean) => {
    if (!value) {
      wizard.closeDockerInstall();
    }
  },
});

const running = computed<boolean>(() => wizard.dockerInstall.status.value === "running");

/** paneState maps the stream lifecycle onto the shared log pane's states. */
const paneState = computed<InstallLogPaneState>(() => {
  const status = wizard.dockerInstall.status.value;
  const count = wizard.dockerInstall.lines.value.length;
  if (status === "error") {
    return "error";
  }
  if (status === "running") {
    return count === 0 ? "empty-waiting" : "ready";
  }
  if (status === "done") {
    return count === 0 ? "empty" : "ready";
  }
  return "idle";
});

/** reload re-runs the install, so "Refresh log" always shows fresh output. */
const reload = (): void => {
  void wizard.dockerInstall.start();
};
</script>

<template>
  <NModal
    v-model:show="show"
    preset="card"
    :title="$t('servers.wizard.dockerInstallTitle')"
    class="app-modal"
    style="width: 720px; max-width: 96vw"
    :mask-closable="false"
  >
    <div class="docker-install-body">
      <NText depth="2">{{ $t("servers.wizard.dockerInstallHint") }}</NText>

      <NAlert v-if="wizard.dockerInstall.error.value" type="error" :show-icon="true">
        {{ wizard.dockerInstall.error.value }}
      </NAlert>

      <InstallJobLogPane
        :title="$t('servers.wizard.dockerInstallLog')"
        :state="paneState"
        :lines="wizard.dockerInstall.lines.value"
        @reload="reload"
      />
    </div>

    <template #footer>
      <div class="docker-install-foot">
        <span class="grow" />
        <NButton @click="wizard.closeDockerInstall()">
          {{ $t("servers.wizard.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="running"
          :disabled="running"
          @click="wizard.recheckAfterDockerInstall()"
        >
          {{ $t("servers.wizard.dockerInstallRecheck") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.docker-install-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.docker-install-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.grow {
  flex: 1;
  min-width: 0;
}
</style>
