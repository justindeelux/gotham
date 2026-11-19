<script setup lang="ts">
import { NCard } from "naive-ui";
import { computed } from "vue";
import { RouterLink } from "vue-router";

import type { Project } from "@/features/projects/api/projects";
import { projectResourceTotal } from "@/features/projects/api/projects";
import ProjectIcon from "@/features/projects/components/ProjectIcon.vue";
import { activeLocale, i18n } from "@/shared/i18n";

interface Props {
  project: Project;
  showStats?: boolean;
}

const props = withDefaults(defineProps<Props>(), { showStats: true });

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

function unit(one: string, other: string, count: number): string {
  return t(count === 1 ? one : other, { count });
}

const stats = computed<string>(() => {
  if (!props.showStats) {
    return "";
  }
  const environments = unit(
    "projects.list.environmentsOne",
    "projects.list.environmentsOther",
    props.project.environment_count,
  );
  const resources = unit(
    "projects.list.resourcesOne",
    "projects.list.resourcesOther",
    projectResourceTotal(props.project),
  );
  return `${environments} · ${resources}`;
});
</script>

<template>
  <RouterLink
    class="project-card-link"
    :to="{ name: 'project-detail', params: { projectId: props.project.id } }"
    :aria-label="t('projects.list.openProject', { name: props.project.name })"
  >
    <NCard size="small" hoverable class="project-card">
      <div class="project-card__top">
        <ProjectIcon :project="props.project" size="md" />
        <span class="project-card__name" :title="props.project.name">
          {{ props.project.name }}
        </span>
      </div>
      <p
        class="project-card__desc"
        :title="props.project.description || undefined"
      >
        {{ props.project.description }}
      </p>
      <div class="project-card__stats">{{ stats }}</div>
    </NCard>
  </RouterLink>
</template>

<style scoped>
.project-card-link {
  display: block;
  min-width: 0;
  border-radius: var(--radius-md);
  color: inherit;
  text-decoration: none;
}

.project-card-link:focus-visible {
  outline: none;
  box-shadow: var(--focus-ring);
}

.project-card {
  height: 100%;
}

.project-card__top {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.project-card__name {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--fg-2);
  font-size: var(--text-base);
  font-weight: 600;
  line-height: var(--leading-title);
}

/* Two clamped lines always reserve their space, so cards without a description match. */
.project-card__desc {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  margin: var(--space-3) 0 0;
  min-height: calc(2em * var(--leading-body));
  color: var(--muted);
  font-size: var(--text-sm);
  line-height: var(--leading-body);
  overflow-wrap: anywhere;
}

/* Same trick for the stats row so showStats=false keeps the height. */
.project-card__stats {
  overflow: hidden;
  margin-top: var(--space-2);
  min-height: calc(var(--text-xs) * var(--leading-tight));
  color: var(--meta);
  font-size: var(--text-xs);
  line-height: var(--leading-tight);
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
