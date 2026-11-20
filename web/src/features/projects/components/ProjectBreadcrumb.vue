<script setup lang="ts">
import { computed } from "vue";
import { RouterLink } from "vue-router";

import { activeLocale, i18n } from "@/shared/i18n";

const props = defineProps<{
  projectName: string;
  projectId: string;
  environmentName?: string;
  environmentId?: string;
  resourceName?: string;
}>();

interface Crumb {
  label: string;
  to: { name: string; params?: Record<string, string> } | null;
  current: boolean;
}

const landmarkLabel = computed<string>(() => {
  void activeLocale.value;
  return String(i18n.global.t("projects.breadcrumb.label"));
});

const crumbs = computed<Crumb[]>(() => {
  void activeLocale.value;
  const trail: Crumb[] = [
    {
      label: String(i18n.global.t("projects.breadcrumb.projects")),
      to: { name: "projects" },
      current: false,
    },
    {
      label: props.projectName,
      to: { name: "project-detail", params: { projectId: props.projectId } },
      current: !props.environmentName,
    },
  ];
  if (props.environmentName) {
    trail.push({
      label: props.environmentName,
      to:
        props.environmentId && props.resourceName
          ? {
              name: "environment-detail",
              params: {
                projectId: props.projectId,
                environmentId: props.environmentId,
              },
            }
          : null,
      current: !props.resourceName,
    });
  }
  if (props.resourceName) {
    trail.push({ label: props.resourceName, to: null, current: true });
  }
  return trail;
});
</script>

<template>
  <nav class="crumb-trail" :aria-label="landmarkLabel">
    <template v-for="(crumb, index) in crumbs" :key="`${crumb.label}-${index}`">
      <span v-if="index > 0" class="crumb-trail__sep" aria-hidden="true">/</span>
      <RouterLink
        v-if="crumb.to && !crumb.current"
        class="link"
        :to="crumb.to"
        :title="crumb.label"
      >
        {{ crumb.label }}
      </RouterLink>
      <span v-else class="current" aria-current="page" :title="crumb.label">
        {{ crumb.label }}
      </span>
    </template>
  </nav>
</template>

<style scoped>
.link {
  color: var(--muted);
  text-decoration: none;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.link:hover {
  color: var(--fg-2);
}

.current {
  color: var(--fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
