<script setup lang="ts">
import { NButton, NPopconfirm, NTag } from "naive-ui";
import { RouterLink, useRouter } from "vue-router";

import { serviceStatusTagType } from "@/features/services/api/services";
import type { Service } from "@/features/services/api/services";
import { useServicesPageContext } from "@/features/services/composables/useServicesList";
import { cardMark } from "@/features/services/utils/serviceDisplay";
import { relativeTime } from "@/shared/utils/format";

interface Props {
  service: Service;
}

/** ServiceCard renders one compose service card in the list grid. */
defineProps<Props>();
const router = useRouter();
const { serverNameOf, deploySummary, deployTagType, handleDelete } =
  useServicesPageContext();
</script>

<template>
  <article
    class="svc-card"
    :data-service="service.name"
  >
    <header class="svc-card__head">
      <span class="avatar" aria-hidden="true">{{ cardMark(service.name) }}</span>
      <div class="svc-card__title">
        <h3 class="mono">
          <RouterLink
            :to="{ name: 'service-detail', params: { id: service.id } }"
          >
            {{ service.name }}
          </RouterLink>
        </h3>
        <p class="small muted">
          {{ serverNameOf(service) }} ·
          <span class="mono">{{ service.compose_project }}</span>
        </p>
      </div>
      <NTag size="small" :type="serviceStatusTagType(service.status)">
        {{ service.status }}
      </NTag>
    </header>

    <div class="svc-card__body">
      <div class="tagrow">
        <NTag
          size="small"
          :type="deployTagType(service)"
          :data-history="deploySummary(service)"
        >
          {{ deploySummary(service) }}
        </NTag>
        <NTag
          v-for="route in service.domains"
          :key="route.domain"
          size="small"
          class="mono"
        >
          {{ route.domain }}:{{ route.port }}
        </NTag>
        <NTag v-if="service.domains.length === 0" size="small">
          no routed domain
        </NTag>
      </div>
      <p v-if="service.domains.length > 0" class="small muted">
        Routed through the node's Traefik proxy by the
        <span class="mono">gotham.domain</span> label.
      </p>
    </div>

    <footer class="svc-card__foot">
      <span class="small muted grow">
        updated {{ relativeTime(service.updated_at) }}
      </span>
      <NButton
        size="small"
        @click="router.push({ name: 'service-detail', params: { id: service.id } })"
      >
        Open detail
      </NButton>
      <NPopconfirm
        :positive-button-props="{ type: 'error' }"
        @positive-click="handleDelete(service)"
      >
        <template #trigger>
          <NButton size="small" type="error" ghost>Delete</NButton>
        </template>
        Delete the service {{ service.name }}? The project is taken
        down; its named volumes stay on the node.
      </NPopconfirm>
    </footer>
  </article>
</template>

<style scoped>
.svc-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-4);
}

.svc-card__head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
}

.avatar {
  width: 34px;
  height: 34px;
  border-radius: var(--radius-md);
  display: grid;
  place-items: center;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-xs);
  color: var(--fg-2);
  flex: 0 0 auto;
}

.svc-card__title {
  flex: 1 1 auto;
  min-width: 0;
}

.svc-card__title h3 {
  margin: 0;
  font-size: var(--text-base);
  font-weight: 600;
}

.svc-card__body {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.tagrow {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.svc-card__foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  border-top: 1px solid var(--border);
  padding-top: var(--space-3);
}

.grow {
  flex: 1 1 auto;
}

.small {
  font-size: var(--text-xs);
}

.muted {
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}
</style>
