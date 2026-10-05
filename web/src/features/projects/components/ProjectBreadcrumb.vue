<script setup lang="ts">
import { computed } from "vue";
import { RouterLink } from "vue-router";

/**
 * Breadcrumb for the project surfaces: `Projects / <project>` on the detail
 * page, `Projects / <project> / <environment>` once PE-5 adds the
 * environment page. Long names truncate with a native tooltip.
 */
const props = defineProps<{
  projectName: string;
  projectId: string;
  environmentName?: string;
}>();

interface Crumb {
  label: string;
  to: { name: string; params?: Record<string, string> } | null;
  current: boolean;
}

const crumbs = computed<Crumb[]>(() => {
  const trail: Crumb[] = [
    { label: "Projects", to: { name: "projects" }, current: false },
    {
      label: props.projectName,
      to: { name: "project-detail", params: { projectId: props.projectId } },
      current: !props.environmentName,
    },
  ];
  if (props.environmentName) {
    trail.push({ label: props.environmentName, to: null, current: true });
  }
  return trail;
});
</script>

<template>
  <nav class="breadcrumb" aria-label="Breadcrumb">
    <template v-for="(crumb, index) in crumbs" :key="`${crumb.label}-${index}`">
      <span v-if="index > 0" class="sep" aria-hidden="true">/</span>
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
.breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0 0 var(--space-2);
  min-width: 0;
}

.sep {
  color: var(--meta);
}

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
