<script setup lang="ts">
import { NAlert, NSpace, NSpin } from "naive-ui";

import ProjectBreadcrumb from "@/features/projects/components/ProjectBreadcrumb.vue";

const props = withDefaults(
  defineProps<{
    loading: boolean;
    breadcrumbLabel: string;
    fallbackId: string;
    breadcrumbs?: {
      projectName: string;
      projectId: string;
      environmentName?: string;
      environmentId?: string;
      resourceName?: string;
    } | null;
    error?: string | null;
    notFound?: boolean;
    notFoundText?: string;
  }>(),
  {
    breadcrumbs: null,
    error: null,
    notFound: false,
    notFoundText: "",
  },
);
</script>

<template>
  <NSpace vertical :size="16">
    <ProjectBreadcrumb v-if="props.breadcrumbs" v-bind="props.breadcrumbs" />
    <nav v-else class="breadcrumb" :aria-label="props.breadcrumbLabel">
      <span class="muted mono">{{ props.fallbackId }}</span>
    </nav>

    <NSpin :show="props.loading">
      <div v-if="props.error || $slots.banner" class="page-banners">
        <NAlert v-if="props.error" type="error" :show-icon="true">
          {{ props.error }}
        </NAlert>
        <template v-if="!props.notFound">
          <slot name="banner" />
        </template>
      </div>
      <slot name="header" />

      <template v-if="props.notFound">
        <slot name="not-found">
          <NAlert v-if="props.notFoundText" type="warning" :show-icon="true" class="frame-alert">
            {{ props.notFoundText }}
          </NAlert>
        </slot>
      </template>
      <slot v-else />
    </NSpin>
  </NSpace>
</template>

<style scoped>
.breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-bottom: var(--space-2);
  font-size: var(--text-xs);
}

.muted {
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}

.frame-alert {
  margin-bottom: var(--space-3);
}
</style>
