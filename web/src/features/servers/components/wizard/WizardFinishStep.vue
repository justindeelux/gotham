<script setup lang="ts">
import { NTag, NText } from "naive-ui";
import { computed, inject } from "vue";

import { WizardKey } from "@/features/servers/composables/useAddServerWizard";
import ServerStatusTag from "@/features/servers/components/ServerStatusTag.vue";

const wizard = inject(WizardKey);
if (!wizard) {
  throw new Error("WizardFinishStep must be used inside AddServerWizard.");
}

/** statusName renders the current lifecycle state for the finish hint. */
const statusName = computed<string>(() =>
  wizard.currentServer.value ? wizard.currentServer.value.status : "pending",
);
</script>

<template>
  <div class="finish">
    <span class="avatar avatar--lg">{{ wizard.nodeInitials.value }}</span>
    <h4>{{ $t("servers.wizard.finishPassed", { name: wizard.currentServer.value?.name ?? wizard.form.name }) }}</h4>
    <NText depth="3">
      {{ $t("servers.wizard.finishHint", { status: $t(`servers.status.${statusName}`) }) }}
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
