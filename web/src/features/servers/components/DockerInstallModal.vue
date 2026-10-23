<script setup lang="ts">
import { NAlert, NButton, NModal, NPopconfirm, NText } from "naive-ui";
import { computed, inject } from "vue";

import { WizardKey } from "@/features/servers/composables/useAddServerWizard";
import InstallJobLogPane from "@/features/toolchain/components/InstallJobLogPane.vue";
import type { InstallLogPaneState } from "@/features/toolchain/components/InstallJobLogPane.vue";

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
</script>

<template>
  <NModal
    v-model:show="show"
    preset="card"
    :title="$t('servers.wizard.dockerInstallTitle')"
    class="app-modal docker-install-modal"
    :mask-closable="false"
    :close-on-esc="!running"
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
        :show-reload="false"
      />
    </div>

    <template #footer>
      <div class="docker-install-foot">
        <span class="grow" />
        <NPopconfirm v-if="running" @positive-click="wizard.closeDockerInstall()">
          <template #trigger>
            <NButton>{{ $t("servers.wizard.dockerInstallStop") }}</NButton>
          </template>
          {{ $t("servers.wizard.dockerInstallStopConfirm") }}
        </NPopconfirm>
        <NButton v-else @click="wizard.closeDockerInstall()">
          {{ $t("servers.wizard.dockerInstallClose") }}
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
.docker-install-modal {
  width: min(var(--modal-width), 96vw);
}

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
