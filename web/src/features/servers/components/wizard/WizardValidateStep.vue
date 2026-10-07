<script setup lang="ts">
import { NAlert, NCheckbox, NSpace, NText } from "naive-ui";
import { computed, inject } from "vue";

import { WizardKey } from "@/features/servers/composables/useAddServerWizard";
import WizardCheckRow from "@/features/servers/components/wizard/WizardCheckRow.vue";

const wizard = inject(WizardKey);
if (!wizard) {
  throw new Error("WizardValidateStep must be used inside AddServerWizard.");
}

/** dotClass renders the SSH status dot for the current probe state. */
const dotClass = computed<string>(() => {
  if (wizard.validating.value) {
    return "dot--running";
  }
  if (wizard.hasRunChecks.value) {
    return wizard.validationPassed.value ? "dot--ok" : "dot--fail";
  }
  return "dot--idle";
});
</script>

<template>
  <NSpace vertical :size="12">
    <div class="status-line-row">
      <NText strong>{{ $t("servers.wizard.validateTitle") }}</NText>
      <span v-if="wizard.sshStatusLine.value" class="status-line">
        <span class="dot" :class="dotClass" />
        {{ wizard.sshStatusLine.value }}
      </span>
    </div>

    <NAlert v-if="wizard.validateMessage.value && !wizard.validationPassed.value" type="error" :show-icon="true">
      {{ wizard.validateMessage.value }}
    </NAlert>

    <NCheckbox
      v-if="wizard.currentServer.value?.has_password"
      v-model:checked="wizard.form.trustHostKey"
    >
      {{ $t("servers.wizard.trustHostKey") }}
    </NCheckbox>

    <div class="check-list">
      <WizardCheckRow
        v-for="check in wizard.fixedChecks.value"
        :key="check.name"
        :state="check.state"
        :label="check.label"
        :detail="check.detail"
      />
    </div>
    <NText depth="3">{{ wizard.checkSummary.value }}</NText>

    <NAlert v-if="wizard.validationPassed.value" type="success" :show-icon="true">
      {{ $t("servers.wizard.validatePassed") }}
    </NAlert>
  </NSpace>
</template>

<style scoped>
.status-line-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.status-line {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
  color: var(--muted);
  font-family: var(--font-mono);
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
  flex: 0 0 auto;
}

.dot--idle {
  background: var(--meta);
}

.dot--running {
  background: var(--accent);
  animation: wizard-pulse 900ms ease-in-out infinite alternate;
}

.dot--ok {
  background: var(--success);
}

.dot--fail {
  background: var(--danger);
}

@keyframes wizard-pulse {
  from {
    opacity: 0.4;
  }
  to {
    opacity: 1;
  }
}

.check-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
</style>
