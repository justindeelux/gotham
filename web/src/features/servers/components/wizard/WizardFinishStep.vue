<script setup lang="ts">
import { NTag, NText } from "naive-ui";
import { inject } from "vue";

import { WizardKey } from "@/features/servers/composables/useAddServerWizard";
import ServerStatusTag from "@/features/servers/components/ServerStatusTag.vue";

const wizard = inject(WizardKey);
if (!wizard) {
  throw new Error("WizardFinishStep must be used inside AddServerWizard.");
}
</script>

<template>
  <div class="finish">
    <span class="avatar avatar--lg">{{ wizard.nodeInitials.value }}</span>
    <h4>{{ wizard.currentServer.value?.name ?? wizard.form.name }} passed validation</h4>
    <NText depth="3">
      Install the agent on the node and it will check in over gRPC.
      The server shows Ready only after its first heartbeat.
    </NText>
    <div class="finish-tags">
      <ServerStatusTag v-if="wizard.currentServer.value" :status="wizard.currentServer.value.status" />
      <NTag v-if="wizard.currentServer.value?.docker_version" size="small" round>
        Docker {{ wizard.currentServer.value.docker_version }}
      </NTag>
      <NTag v-if="wizard.resourceSummary.value" size="small" round>
        {{ wizard.resourceSummary.value }}
      </NTag>
    </div>
  </div>
</template>

<style scoped>
.finish {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  text-align: center;
  padding: var(--space-6) 0;
}

.finish h4 {
  margin: 0;
  color: var(--fg-2);
  font-size: var(--text-xl);
}

.finish-tags {
  display: flex;
  gap: var(--space-2);
  flex-wrap: wrap;
  justify-content: center;
}

.avatar {
  display: inline-grid;
  place-items: center;
  background: var(--accent-soft);
  color: var(--accent-ink);
  font-weight: 700;
  border-radius: var(--radius-md);
}

.avatar--lg {
  width: 48px;
  height: 48px;
  font-size: var(--text-lg);
}
</style>
