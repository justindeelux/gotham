<script setup lang="ts">
import { NTag } from "naive-ui";
import { computed } from "vue";

import { serviceBrand } from "@/features/add-resource";
import ResourceActionButton from "@/features/projects/components/ResourceActionButton.vue";
import ResourceHeader from "@/features/projects/components/ResourceHeader.vue";
import { serviceStatusLabel, serviceStatusTagType } from "@/features/services/api/services";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import { activeLocale, i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const {
  service,
  serviceId,
  busy,
  controlHint,
  latestDeploy,
  handleDeploy,
  handleRestart,
  handleStop,
  handleStart,
} = useServiceDetailContext();

const deployLabel = computed<string>(() =>
  latestDeploy.value ? t("services.header.redeploy") : t("services.header.deploy"),
);

const brand = computed(() => (service.value ? serviceBrand(service.value) : null));
</script>

<template>
  <ResourceHeader
    :title="service?.name ?? serviceId.slice(0, 8)"
    :resource-id="serviceId"
    :brand="brand"
  >
    <template #badges>
      <NTag v-if="service" size="medium" round :type="serviceStatusTagType(service.status)">
        {{ serviceStatusLabel(service.status) }}
      </NTag>
    </template>
    <template #actions>
      <template v-if="service">
        <ResourceActionButton
          type="primary"
          icon="refresh"
          :label="deployLabel"
          :loading="busy === 'deploy'"
          :disabled="busy !== null"
          :disabled-reason="controlHint"
          @click="handleDeploy"
        />
        <ResourceActionButton
          type="default"
          icon="refresh"
          :label="t('services.header.restart')"
          :loading="busy === 'restart'"
          :disabled="busy !== null"
          :disabled-reason="controlHint"
          @click="handleRestart"
        />
        <ResourceActionButton
          type="error"
          icon="stop"
          :label="t('services.header.stop')"
          :loading="busy === 'stop'"
          :disabled="busy !== null || service.status === 'stopped'"
          :disabled-reason="controlHint"
          @click="handleStop"
        />
        <ResourceActionButton
          type="success"
          icon="play"
          :label="t('services.header.start')"
          :loading="busy === 'start'"
          :disabled="busy !== null || service.status !== 'stopped'"
          :disabled-reason="controlHint"
          @click="handleStart"
        />
      </template>
    </template>
  </ResourceHeader>
</template>
