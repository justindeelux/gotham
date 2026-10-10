<script setup lang="ts">
import { NAvatar, NButton, NIcon, NSpace, NTag, NText, NTooltip } from "naive-ui";
import { useI18n } from "vue-i18n";

import type { Deployment } from "@/features/applications/api/applications";
import DeploymentStatusTag from "@/features/applications/components/DeploymentStatusTag.vue";
import GothamIcon from "@/shared/ui/GothamIcon.vue";

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

const { t } = useI18n();
</script>

<template>
  <div class="page-head">
    <NAvatar round :size="48">{{ props.initials }}</NAvatar>
    <div class="page-head__title">
      <NSpace align="center" :size="10">
        <NText strong style="font-size: 20px" class="mono">{{ props.displayName || t("applications.header.fallbackName") }}</NText>
        <DeploymentStatusTag
          v-if="props.latest && !props.containerStopped"
          :state="props.latest.state"
          size="medium"
        />
        <NTag v-else-if="props.containerStopped" type="default" size="medium" round>
          {{ t("applications.header.stopped") }}
        </NTag>
      </NSpace>
      <NText depth="3" class="mono">{{ props.appId }}</NText>
    </div>
    <NSpace class="page-head__actions" align="center" :size="8">
      <NButton
        type="primary"
        ghost
        :loading="props.acting"
        :disabled="props.activeDeploying"
        @click="emit('deploy')"
      >
        <template #icon>
          <NIcon><GothamIcon name="refresh" /></NIcon>
        </template>
        {{ props.activeDeploying ? t("applications.header.deploying") : t("applications.header.redeploy") }}
      </NButton>
      <NButton
        type="warning"
        ghost
        :disabled="!props.canRollback || props.acting"
        @click="emit('rollback')"
      >
        <template #icon>
          <NIcon><GothamIcon name="history" /></NIcon>
        </template>
        {{ t("applications.header.rollback") }}
      </NButton>
      <NTooltip trigger="hover" :disabled="props.controlHint === null">
        <template #trigger>
          <NButton
            type="error"
            ghost
            :loading="props.acting"
            :disabled="props.acting || props.controlHint !== null || !props.containerIsRunning"
            @click="emit('stop')"
          >
            <template #icon>
              <NIcon><GothamIcon name="stop" /></NIcon>
            </template>
            {{ t("applications.header.stop") }}
          </NButton>
        </template>
        {{ props.controlHint }}
      </NTooltip>
      <NTooltip trigger="hover" :disabled="props.controlHint === null">
        <template #trigger>
          <NButton
            type="success"
            ghost
            :loading="props.acting"
            :disabled="props.acting || props.controlHint !== null || props.containerIsRunning"
            @click="emit('start')"
          >
            <template #icon>
              <NIcon><GothamIcon name="play" /></NIcon>
            </template>
            {{ t("applications.header.start") }}
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
  flex-shrink: 1;
  min-width: 0;
  max-width: 100%;
  flex-wrap: wrap;
  justify-content: flex-end;
}
</style>
