<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";

import type { Project } from "@/features/projects/api/projects";
import { getProjectIcon } from "@/features/projects/api/projects";
import { projectIconPreset } from "@/features/projects/icons";

interface Props {
  project: Pick<Project, "id" | "name" | "icon" | "icon_version">;
  size?: "sm" | "md" | "lg";
}

const props = withDefaults(defineProps<Props>(), { size: "md" });

const imageURL = ref<string | null>(null);
const preset = computed(() => projectIconPreset(props.project.icon));
const initial = computed(
  () => props.project.name.trim().charAt(0).toUpperCase() || "?",
);

let requestID = 0;
let objectURL: string | null = null;

function release(): void {
  if (objectURL) {
    URL.revokeObjectURL(objectURL);
    objectURL = null;
  }
  imageURL.value = null;
}

watch(
  () => [props.project.id, props.project.icon, props.project.icon_version] as const,
  async ([projectId, icon, version]) => {
    release();
    const request = ++requestID;
    if (icon !== "upload") {
      return;
    }
    try {
      const blob = await getProjectIcon(projectId, version);
      if (request !== requestID) {
        return;
      }
      objectURL = URL.createObjectURL(blob);
      imageURL.value = objectURL;
    } catch {
      // Keep the letter fallback; the picker reports write failures.
    }
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  requestID++;
  release();
});
</script>

<template>
  <span
    class="project-icon"
    :class="`project-icon--${props.size}`"
    aria-hidden="true"
  >
    <img v-if="imageURL" class="project-icon__image" :src="imageURL" alt="" />
    <component :is="preset" v-else-if="preset" class="project-icon__glyph" />
    <span v-else class="project-icon__letter">{{ initial }}</span>
  </span>
</template>

<style scoped>
.project-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  overflow: hidden;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-weight: 600;
}

.project-icon--sm {
  width: var(--brand-icon-sm);
  height: var(--brand-icon-sm);
  border-radius: var(--radius-sm);
  font-size: var(--text-xs);
}

.project-icon--md {
  width: var(--brand-icon-md);
  height: var(--brand-icon-md);
  border-radius: var(--radius-md);
  font-size: var(--text-base);
}

.project-icon--lg {
  width: var(--brand-icon-lg);
  height: var(--brand-icon-lg);
  border-radius: var(--radius-lg);
  font-size: var(--text-lg);
}

.project-icon__image {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.project-icon__glyph {
  width: 60%;
  height: 60%;
}
</style>
