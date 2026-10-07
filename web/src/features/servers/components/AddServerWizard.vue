<script setup lang="ts">
import {
  NAlert,
  NButton,
  NInput,
  NModal,
  NSpace,
  NText,
  useMessage,
} from "naive-ui";
import { provide } from "vue";

import type { Server } from "@/features/servers/api/servers";
import ServerStatusTag from "@/features/servers/components/ServerStatusTag.vue";
import WizardCheckRow from "@/features/servers/components/wizard/WizardCheckRow.vue";
import WizardConnectStep from "@/features/servers/components/wizard/WizardConnectStep.vue";
import WizardFinishStep from "@/features/servers/components/wizard/WizardFinishStep.vue";
import WizardValidateStep from "@/features/servers/components/wizard/WizardValidateStep.vue";
import {
  WizardKey,
  useAddServerWizard,
} from "@/features/servers/composables/useAddServerWizard";
import { formatBytes } from "@/shared/utils/format";
import { useI18n } from "vue-i18n";

interface Props {
  show: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
  created: [server: Server];
}>();

const wizard = useAddServerWizard(emit);
provide(WizardKey, wizard);
const {
  step,
  stepNames,
  currentServer,
  isReady,
  passedCount,
  fixedChecks,
  creating,
  validating,
  validationPassed,
  hasCreatedServer,
} = wizard;

const message = useMessage();
const { t } = useI18n();

// The agent installer is not a release asset and needs its sibling files
// (deploy/release-verify.sh, gotham-agent-updater.conf, ...); it also fails
// closed without the control-plane CA, and the agent dials 127.0.0.1 unless
// GOTHAM_AGENT_CP_ADDR is set. This is the same complete checkout block
// docs/install.md uses — keep it a single copy-pasteable command (the copy
// button copies this exact string).
const installCommand = [
  "scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .",
  "git clone --depth 1 https://github.com/justindeelux/gotham /tmp/gotham",
  "sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=<node> \\",
  "  /tmp/gotham/deploy/install-agent.sh --ca ./ca.crt --full",
  "",
  "# --full installs Docker Engine + the compose plugin (Ubuntu/Debian).",
  "# install.sh already adds a localhost agent by default (--no-local-agent",
  "# opts out); use this snippet for every further node.",
  "# The installer verifies the signed manifest + digest, writes",
  "# /etc/gotham/ca.crt, then enables the systemd unit.",
  "systemctl status gotham-agent",
].join("\n");

/** copyInstallCommand copies the agent install block to the clipboard. */
async function copyInstallCommand(): Promise<void> {
  try {
    await navigator.clipboard.writeText(installCommand);
    message.success(t("servers.wizard.installCopied"));
  } catch {
    message.error(t("servers.wizard.copyFailed"));
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="$t('servers.wizard.title')"
    :mask-closable="false"
    class="wizard-modal"
    style="width: 880px; max-width: 96vw"
    @update:show="wizard.handleShowChange"
  >
    <NText depth="3">
      {{ $t("servers.wizard.intro") }}
    </NText>

    <div class="wizard">
      <div class="wizard-rail">
        <ol>
          <li
            v-for="(label, index) in stepNames"
            :key="label"
            :class="{
              'is-active': step === index,
              'is-done': step > index,
            }"
          >
            <span class="idx">{{ index + 1 }}</span>{{ label }}
          </li>
        </ol>
        <p class="wizard-rail-note">
          {{ $t("servers.wizard.secretNote") }}
        </p>
      </div>

      <div class="wizard-main">
        <div class="wizard-body">
          <NAlert v-if="wizard.errorMessage.value" type="error" :show-icon="true">
            {{ wizard.errorMessage.value }}
          </NAlert>

          <!-- Step 1: connection details. Field hints render as
               class="field-hint" inside WizardConnectStep on the shared
               global hint gap (no local hint styles here or there). -->
          <WizardConnectStep v-if="step === 0" />

          <!-- Step 2: validation checklist -->
          <WizardValidateStep v-else-if="step === 1" />

          <!-- Step 3: install the agent (real states only) -->
          <NSpace v-else-if="step === 2" vertical :size="12">
            <NText strong>{{ $t("servers.wizard.installTitle") }}</NText>

            <div class="check-list">
              <WizardCheckRow
                state="ok"
                :label="$t('servers.wizard.registeredRow')"
                :detail="currentServer?.name ?? ''"
              />
              <WizardCheckRow
                :state="validationPassed ? 'ok' : 'idle'"
                :label="$t('servers.wizard.probesRow')"
                :detail="`${passedCount}/${fixedChecks.length}`"
              />
              <WizardCheckRow
                :state="isReady ? 'ok' : validating ? 'running' : 'idle'"
                :label="$t('servers.wizard.agentRow')"
                :detail="isReady ? $t('servers.wizard.agentHeartbeat') : $t('servers.wizard.agentWaiting')"
              />
            </div>

            <NSpace align="center" :size="8">
              <NText depth="2">{{ $t("servers.wizard.currentStatus") }}</NText>
              <ServerStatusTag v-if="currentServer" :status="currentServer.status" />
            </NSpace>

            <NAlert v-if="isReady" type="success" :show-icon="true">
              {{ $t("servers.wizard.installReady") }}
            </NAlert>

            <NText depth="2">
              {{ $t("servers.wizard.installHowto") }}
            </NText>

            <NSpace align="start" :size="8">
              <NInput
                :value="installCommand"
                type="textarea"
                readonly
                :autosize="{ minRows: 8, maxRows: 11 }"
                class="grow install-command"
              />
              <NButton @click="copyInstallCommand">{{ $t("servers.wizard.copy") }}</NButton>
            </NSpace>

            <NText depth="3">
              {{ $t("servers.wizard.pollingNote") }}
            </NText>

            <NText v-if="currentServer" depth="3">
              {{ $t("servers.wizard.detectedMemory", { mem: formatBytes(currentServer.total_mem), disk: formatBytes(currentServer.total_disk) }) }}
            </NText>
          </NSpace>

          <!-- Step 4: finish -->
          <WizardFinishStep v-else />
        </div>

        <div class="wizard-foot">
          <NButton v-if="step > 0" tertiary @click="step -= 1">{{ $t("servers.wizard.back") }}</NButton>
          <span class="step-counter">{{ $t("servers.wizard.stepOf", { current: step + 1 }) }}</span>
          <span class="grow" />
          <template v-if="step === 0">
            <NButton @click="wizard.closeWizard">{{ $t("servers.wizard.cancel") }}</NButton>
            <NButton type="primary" :loading="creating" @click="wizard.handleCreate">
              {{ hasCreatedServer ? $t("servers.wizard.continue") : $t("servers.wizard.createContinue") }}
            </NButton>
          </template>
          <template v-else-if="step === 1">
            <NButton :loading="validating" @click="wizard.handleValidate">
              {{ $t("servers.wizard.retryValidation") }}
            </NButton>
            <NButton
              type="primary"
              :disabled="!validationPassed"
              @click="step = 2"
            >
              {{ $t("servers.wizard.continue") }}
            </NButton>
          </template>
          <template v-else-if="step === 2">
            <NButton type="primary" @click="step = 3">{{ $t("servers.wizard.continueToFinish") }}</NButton>
          </template>
          <template v-else>
            <NButton type="primary" @click="wizard.closeWizard">{{ $t("servers.wizard.done") }}</NButton>
          </template>
        </div>
      </div>
    </div>
  </NModal>
</template>

<style scoped>
.wizard {
  display: grid;
  grid-template-columns: 208px minmax(0, 1fr);
  min-height: 420px;
  margin-top: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
}

.wizard-rail {
  border-right: 1px solid var(--border);
  padding: var(--space-4) var(--space-3);
  background: color-mix(in oklab, var(--surface) 70%, var(--surface-warm));
}

.wizard-rail ol {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
}

.wizard-rail li {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 7px var(--space-2);
  border-radius: var(--radius-sm);
  font-size: var(--text-sm);
  color: var(--muted);
}

.wizard-rail li .idx {
  width: 20px;
  height: 20px;
  border-radius: var(--radius-pill);
  border: 1.5px solid var(--meta);
  display: grid;
  place-items: center;
  font-family: var(--font-mono);
  font-size: 10px;
  flex: 0 0 auto;
}

.wizard-rail li.is-done {
  color: var(--fg);
}

.wizard-rail li.is-done .idx {
  background: var(--success);
  border-color: var(--success);
  color: var(--accent-on);
}

.wizard-rail li.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
  font-weight: 600;
}

.wizard-rail li.is-active .idx {
  border-color: var(--accent);
  color: var(--accent-ink);
}

.wizard-rail-note {
  margin: var(--space-4) 0 0;
  font-size: var(--text-xs);
  color: var(--meta);
  line-height: var(--leading-body);
}

.wizard-main {
  display: flex;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
}

.wizard-body {
  padding: var(--space-4);
  overflow-y: auto;
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.wizard-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border);
  flex: 0 0 auto;
}

.step-counter {
  font-size: var(--text-xs);
  color: var(--muted);
  font-family: var(--font-mono);
}

.check-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.grow {
  flex: 1;
  min-width: 0;
}

.install-command :deep(.n-input__textarea-el),
.install-command :deep(textarea) {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  line-height: var(--leading-body);
}

.wizard-modal :deep(.n-card-content) {
  max-height: 72vh;
  overflow-y: auto;
}

@media (max-width: 720px) {
  .wizard {
    grid-template-columns: minmax(0, 1fr);
  }

  .wizard-rail {
    border-right: 0;
    border-bottom: 1px solid var(--border);
  }

  .wizard-rail ol {
    flex-direction: row;
    flex-wrap: wrap;
  }
}
</style>
