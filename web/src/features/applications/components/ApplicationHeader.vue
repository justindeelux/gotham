<script setup lang="ts">
import { NButton, NIcon, NTag, NTooltip } from "naive-ui";
import { useI18n } from "vue-i18n";

import { BrandIcon } from "@/features/add-resource";
import type { BrandMark } from "@/features/add-resource";
import type { Deployment } from "@/features/applications/api/applications";
import DeploymentStatusTag from "@/features/applications/components/DeploymentStatusTag.vue";
import GothamIcon from "@/shared/ui/GothamIcon.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";

interface Props {
  displayName: string;
  appId: string;
  shortId: string;
  brand: BrandMark;
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
  <PageHeader>
    <template #identity>
      <BrandIcon v-bind="props.brand" size="lg" />
    </template>
    <template #title>{{ props.displayName || t("applications.header.fallbackName") }}</template>
    <template #badges>
      <DeploymentStatusTag
        v-if="props.latest && !props.containerStopped"
        :state="props.latest.state"
        size="medium"
      />
      <NTag v-else-if="props.containerStopped" type="default" size="medium" round>
        {{ t("applications.header.stopped") }}
      </NTag>
    </template>
    <template #meta>{{ props.appId }}</template>
    <template #actions>
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
    </template>
  </PageHeader>
</template>
