<script setup lang="ts">
import { NCard, NSpace, NText } from "naive-ui";
import { RouterLink } from "vue-router";

import type { Project } from "@/features/projects/api/projects";
import {
  projectResourceTotal,
  resourceSummary,
} from "@/features/projects/api/projects";
import { activeLocale, i18n } from "@/shared/i18n";

interface Props {
  project: Project;
}

const props = defineProps<Props>();

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

function unit(one: string, other: string, count: number): string {
  return t(count === 1 ? one : other, { count });
}
</script>

<template>
  <RouterLink
    class="project-card-link"
    :to="{ name: 'project-detail', params: { projectId: props.project.id } }"
    :aria-label="t('projects.list.openProject', { name: props.project.name })"
  >
    <NCard :title="props.project.name" hoverable>
      <template #header-extra>
        <span class="avatar" aria-hidden="true">{{
          props.project.name.charAt(0).toUpperCase()
        }}</span>
      </template>
      <NSpace vertical :size="8">
        <NText v-if="props.project.description" depth="3" class="desc">
          {{ props.project.description }}
        </NText>
        <NText depth="3" class="counts">
          {{ unit("projects.list.environmentsOne", "projects.list.environmentsOther", props.project.environment_count) }}
        </NText>
        <NText depth="3" class="counts">
          {{ resourceSummary(props.project.resource_counts) }}
        </NText>
        <NText depth="3" class="counts">
          {{ unit("projects.list.resourcesTotalOne", "projects.list.resourcesTotalOther", projectResourceTotal(props.project)) }}
        </NText>
      </NSpace>
    </NCard>
  </RouterLink>
</template>

<style scoped>
.project-card-link {
  display: block;
  text-decoration: none;
  color: inherit;
  min-width: 0;
  border-radius: var(--radius-md);
}

.project-card-link:focus-visible {
  outline: none;
  box-shadow: var(--focus-ring);
}

.avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: var(--radius-pill);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-weight: 600;
}

.desc,
.counts {
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
