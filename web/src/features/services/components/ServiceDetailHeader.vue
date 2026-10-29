<script setup lang="ts">
import { NButton, NTag } from "naive-ui";

import ResourceHeader from "@/features/projects/components/ResourceHeader.vue";
import { serviceStatusLabel, serviceStatusTagType } from "@/features/services/api/services";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import { activeLocale, i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const { service, serviceId, busy, handleDeploy, handleRestart, handleStop } =
  useServiceDetailContext();
</script>

<template>
  <ResourceHeader
    :title="service?.name ?? serviceId.slice(0, 8)"
    :resource-id="serviceId"
  >
    <template #badges>
      <NTag v-if="service" size="medium" round :type="serviceStatusTagType(service.status)">
        {{ serviceStatusLabel(service.status) }}
      </NTag>
    </template>
    <template #actions>
      <NButton
        v-if="service"
        type="primary"
        :loading="busy === 'deploy'"
        :disabled="busy !== null"
        @click="handleDeploy"
      >
        {{ t("services.header.deploy") }}
      </NButton>
      <NButton
        v-if="service"
        :loading="busy === 'restart'"
        :disabled="busy !== null"
        @click="handleRestart"
      >
        {{ t("services.header.restart") }}
      </NButton>
      <NButton
        v-if="service"
        :loading="busy === 'stop'"
        :disabled="busy !== null"
        @click="handleStop"
      >
        {{ t("services.header.stop") }}
      </NButton>
    </template>
  </ResourceHeader>
</template>
