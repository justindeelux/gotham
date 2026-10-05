<script setup lang="ts">
import { NButton, NPopconfirm, NSpace, NTag } from "naive-ui";
import { RouterLink } from "vue-router";

import { serviceStatusTagType } from "@/features/services/api/services";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";

/** ServiceDetailHeader renders the back link, title tags and actions. */
const { service, serviceId, serverName, busy, handleDeploy, handleRestart, handleStop, handleDelete } =
  useServiceDetailContext();
</script>

<template>
  <div class="page-head">
    <div class="page-head__title">
      <RouterLink :to="{ name: 'services' }" class="back">
        ← Services
      </RouterLink>
      <h1 class="mono">{{ service?.name ?? serviceId.slice(0, 8) }}</h1>
      <NSpace :size="8" align="center">
        <NTag
          v-if="service"
          size="small"
          :type="serviceStatusTagType(service.status)"
        >
          {{ service.status }}
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
    </div>
    <div class="page-actions">
      <NButton
        v-if="service"
        type="primary"
        :loading="busy === 'deploy'"
        :disabled="busy !== null"
        @click="handleDeploy"
      >
        Deploy
      </NButton>
      <NButton
        v-if="service"
        :loading="busy === 'restart'"
        :disabled="busy !== null"
        @click="handleRestart"
      >
        Restart
      </NButton>
      <NButton
        v-if="service"
        :loading="busy === 'stop'"
        :disabled="busy !== null"
        @click="handleStop"
      >
        Stop
      </NButton>
      <NPopconfirm
        v-if="service"
        :positive-button-props="{ type: 'error' }"
        @positive-click="handleDelete"
      >
        <template #trigger>
          <NButton type="error" ghost :disabled="busy !== null">Delete</NButton>
        </template>
        Delete the service {{ service.name }}? The project goes down; its
        named volumes stay on the node.
      </NPopconfirm>
    </div>
  </div>
</template>

<style scoped>
.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.page-head__title {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
}

.back {
  font-size: var(--text-xs);
  color: var(--muted);
}

.page-head h1 {
  margin: 0;
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  word-break: break-all;
}

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.mono {
  font-family: var(--font-mono);
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
    width: 100%;
  }
}
</style>
