<script setup lang="ts">
import { NTag } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { BrandMark } from "@/features/add-resource";
import type { Deployment } from "@/features/applications/api/applications";
import DeploymentStatusTag from "@/features/applications/components/DeploymentStatusTag.vue";
import ResourceActionButton from "@/features/projects/components/ResourceActionButton.vue";
import ResourceHeader from "@/features/projects/components/ResourceHeader.vue";

interface Props {
  displayName: string;
  appId: string;
  brand: BrandMark;
  latest: Deployment | null;
  containerStopped: boolean;
  activeDeploying: boolean;
  canRollback: boolean;
  acting: boolean;
  controlHint: string | null;
  containerIsRunning: boolean;
  canWrite?: boolean;
}

const props = withDefaults(defineProps<Props>(), { canWrite: true });

const emit = defineEmits<{
  deploy: [];
  rollback: [];
  stop: [];
  start: [];
}>();

const { t } = useI18n();

const readOnlyHint = computed<string | null>(() =>
  props.canWrite ? null : t("applications.detail.controlReadOnly"),
);

const deployHint = computed<string | null>(() =>
  props.activeDeploying
    ? t("applications.detail.controlInProgress")
    : readOnlyHint.value,
);

const controlHint = computed<string | null>(
  () => props.controlHint ?? readOnlyHint.value,
);
</script>

<template>
  <ResourceHeader
    :title="props.displayName || t('applications.header.fallbackName')"
    :resource-id="props.appId"
    :brand="props.brand"
  >
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
    <template #actions>
      <ResourceActionButton
        type="primary"
        icon="refresh"
        :label="props.activeDeploying ? t('applications.header.deploying') : t('applications.header.redeploy')"
        :loading="props.acting"
        :disabled="props.activeDeploying"
        :disabled-reason="deployHint"
        @click="emit('deploy')"
      />
      <ResourceActionButton
        type="warning"
        icon="history"
        :label="t('applications.header.rollback')"
        :disabled="!props.canRollback || props.acting"
        :disabled-reason="readOnlyHint"
        @click="emit('rollback')"
      />
      <ResourceActionButton
        type="error"
        icon="stop"
        :label="t('applications.header.stop')"
        :loading="props.acting"
        :disabled="props.acting || !props.containerIsRunning"
        :disabled-reason="controlHint"
        @click="emit('stop')"
      />
      <ResourceActionButton
        type="success"
        icon="play"
        :label="t('applications.header.start')"
        :loading="props.acting"
        :disabled="props.acting || props.containerIsRunning"
        :disabled-reason="controlHint"
        @click="emit('start')"
      />
    </template>
  </ResourceHeader>
</template>
