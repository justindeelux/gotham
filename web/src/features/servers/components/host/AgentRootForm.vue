<script setup lang="ts">
import { NAlert, NButton, NCard, NModal, NSwitch } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import { saveNodeAgentRoot } from "@/features/servers/api/host";
import type { NodeHostState } from "@/features/servers/api/host";
import { errorCode } from "@/shared/api/errors";
import { useSectionForm } from "@/shared/composables/useSectionForm";

const props = defineProps<{ serverId: string; state: NodeHostState }>();
const emit = defineEmits<{ saved: [serverId: string, state: NodeHostState] }>();
const { t } = useI18n();

const showConfirm = ref(false);
const pending = ref<boolean | null>(null);

const disabled = computed(() => props.state.capability !== "ready");

async function saveAgentRoot(): Promise<NodeHostState> {
  try {
    return await saveNodeAgentRoot(props.serverId, pending.value ?? props.state.agent_root);
  } catch (error) {
    if (errorCode(error) === "agent_restarting") {
      throw new Error(t("servers.settings.host.agentRoot.restarting"), { cause: error });
    }
    throw error;
  }
}

const { submitting, errorMessage, submit } = useSectionForm(
  saveAgentRoot,
  (next) => emit("saved", props.serverId, next),
  () =>
    t(
      pending.value
        ? "servers.settings.host.agentRoot.enabledSaved"
        : "servers.settings.host.agentRoot.disabledSaved",
    ),
);

function requestToggle(enabled: boolean): void {
  if (disabled.value) {
    return;
  }
  pending.value = enabled;
  showConfirm.value = true;
}

async function confirmToggle(): Promise<void> {
  await submit();
  showConfirm.value = false;
}
</script>

<template>
  <NCard :title="t('servers.settings.host.agentRoot.title')">
    <NAlert v-if="errorMessage" type="error" :show-icon="true" class="form-alert form-alert-multiline">
      {{ errorMessage }}
    </NAlert>
    <p class="field-hint">{{ t("servers.settings.host.agentRoot.hint") }}</p>
    <div class="agent-root-row">
      <NSwitch
        :value="state.agent_root"
        :disabled="disabled"
        :aria-label="t('servers.settings.host.agentRoot.toggleLabel')"
        @update:value="requestToggle"
      />
      <span>
        {{
          state.agent_root
            ? t("servers.settings.host.agentRoot.on")
            : t("servers.settings.host.agentRoot.off")
        }}
      </span>
    </div>

    <NModal
      v-model:show="showConfirm"
      preset="card"
      :title="t('servers.settings.host.agentRoot.confirmTitle')"
      class="app-modal standard-width"
    >
      <NAlert v-if="pending === true" type="error" :show-icon="true" class="form-alert">
        {{ t("servers.settings.host.agentRoot.rootWarning") }}
      </NAlert>
      <p v-else>{{ t("servers.settings.host.agentRoot.disableBody") }}</p>
      <p class="field-hint">{{ t("servers.settings.host.agentRoot.restartHint") }}</p>
      <template #footer>
        <NButton @click="showConfirm = false">
          {{ t("servers.settings.host.agentRoot.cancel") }}
        </NButton>
        <NButton type="primary" :loading="submitting" @click="confirmToggle">
          {{ t("servers.settings.host.agentRoot.confirm") }}
        </NButton>
      </template>
    </NModal>
  </NCard>
</template>

<style scoped>
.agent-root-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
</style>
