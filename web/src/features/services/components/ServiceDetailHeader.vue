<script setup lang="ts">
import { NButton, NPopconfirm, NSpace, NTag } from "naive-ui";
import { RouterLink } from "vue-router";

import { serviceStatusLabel, serviceStatusTagType } from "@/features/services/api/services";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import PageHeader from "@/shared/ui/PageHeader.vue";
import { activeLocale, i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const { service, serviceId, serverName, busy, handleDeploy, handleRestart, handleStop, handleDelete } =
  useServiceDetailContext();
</script>

<template>
  <PageHeader>
    <template #breadcrumb>
      <RouterLink
        :to="
          service
            ? {
                name: 'environment-detail',
                params: {
                  projectId: service.project_id,
                  environmentId: service.environment_id,
                },
              }
            : { name: 'projects' }
        "
        class="back"
      >
        ← {{ service?.environment_name ?? t("services.header.backFallback") }}
      </RouterLink>
    </template>
    <template #title>{{ service?.name ?? serviceId.slice(0, 8) }}</template>
    <template #badges>
      <NSpace :size="8" align="center">
        <NTag
          v-if="service"
          size="small"
          :type="serviceStatusTagType(service.status)"
        >
          {{ serviceStatusLabel(service.status) }}
        </NTag>
        <NTag v-if="service" size="small">{{ serverName }}</NTag>
        <NTag
          v-for="route in service?.domains ?? []"
          :key="route.domain"
          size="small"
          class="mono"
        >
          {{ route.domain }}:{{ route.port }}
        </NTag>
        <NTag v-if="service" size="small" class="mono">
          {{ service.compose_project }}
        </NTag>
      </NSpace>
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
      <NPopconfirm
        v-if="service"
        :positive-button-props="{ type: 'error' }"
        @positive-click="handleDelete"
      >
        <template #trigger>
          <NButton type="error" ghost :disabled="busy !== null">{{ t("services.header.delete") }}</NButton>
        </template>
        {{ t("services.header.deleteConfirm", { name: service.name }) }}
      </NPopconfirm>
    </template>
  </PageHeader>
</template>

<style scoped>
.back {
  font-size: var(--text-xs);
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}
</style>
