<script setup lang="ts">
import { NAvatar, NButton, NSpace, NTag, NText, NTooltip } from "naive-ui";

import type { Deployment } from "@/features/applications/api/applications";
import DeploymentStatusTag from "@/features/applications/components/DeploymentStatusTag.vue";

interface Props {
  displayName: string;
  appId: string;
  shortId: string;
  initials: string;
  latest: Deployment | null;
  containerStopped: boolean;
  activeDeploying: boolean;
  canRollback: boolean;
  acting: boolean;
  controlHint: string | null;
  containerIsRunning: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  deploy: [];
  rollback: [];
  stop: [];
  start: [];
}>();
</script>

<template>
  <div class="page-head">
    <NAvatar round :size="48">{{ props.initials }}</NAvatar>
    <div class="page-head__title">
      <NSpace align="center" :size="10">
        <NText strong style="font-size: 20px" class="mono">{{ props.displayName || "Application" }}</NText>
        <DeploymentStatusTag
          v-if="props.latest && !props.containerStopped"
          :state="props.latest.state"
          size="medium"
        />
        <NTag v-else-if="props.containerStopped" type="default" size="medium" round>
          stopped
        </NTag>
      </NSpace>
      <NText depth="3" class="mono">{{ props.appId }}</NText>
    </div>
    <NSpace class="page-head__actions" align="center" :size="8">
      <NButton
        :loading="props.acting"
        :disabled="props.activeDeploying"
        @click="emit('deploy')"
      >
        {{ props.activeDeploying ? "Deploying…" : "Redeploy" }}
      </NButton>
      <NButton
        :disabled="!props.canRollback || props.acting"
        @click="emit('rollback')"
      >
        Rollback
      </NButton>
      <NTooltip trigger="hover" :disabled="props.controlHint === null">
        <template #trigger>
          <NButton
            :loading="props.acting"
            :disabled="props.acting || props.controlHint !== null || !props.containerIsRunning"
            @click="emit('stop')"
          >
            Stop
          </NButton>
        </template>
        {{ props.controlHint }}
      </NTooltip>
      <NTooltip trigger="hover" :disabled="props.controlHint === null">
        <template #trigger>
          <NButton
            :loading="props.acting"
            :disabled="props.acting || props.controlHint !== null || props.containerIsRunning"
            @click="emit('start')"
          >
            Start
          </NButton>
        </template>
        {{ props.controlHint }}
      </NTooltip>
    </NSpace>
  </div>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.page-head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.page-head__title {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  flex: 1;
  min-width: 0;
}

.page-head__actions {
  margin-left: auto;
  flex-shrink: 0;
}
</style>
