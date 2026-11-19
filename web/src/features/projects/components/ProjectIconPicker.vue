<script setup lang="ts">
import { NButton, NSpace, NText } from "naive-ui";
import { computed, ref } from "vue";

import ProjectIcon from "@/features/projects/components/ProjectIcon.vue";
import type { Project } from "@/features/projects/api/projects";
import {
  clearProjectIcon,
  describeProjectError,
  setProjectIcon,
  uploadProjectIcon,
} from "@/features/projects/api/projects";
import { projectIconPresets } from "@/features/projects/icons";
import { activeLocale, i18n } from "@/shared/i18n";

interface Props {
  project: Project;
  disabled?: boolean;
}

const props = withDefaults(defineProps<Props>(), { disabled: false });
const emit = defineEmits<{ updated: [project: Project] }>();

const fileInput = ref<HTMLInputElement | null>(null);
const busy = ref(false);
const errorMessage = ref("");

const currentPreset = computed(() =>
  props.project.icon.startsWith("preset:")
    ? props.project.icon.slice("preset:".length)
    : "",
);

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

async function run(action: () => Promise<Project>): Promise<void> {
  if (busy.value) {
    return;
  }
  busy.value = true;
  errorMessage.value = "";
  try {
    emit("updated", await action());
  } catch (error) {
    errorMessage.value = describeProjectError(error);
  } finally {
    busy.value = false;
  }
}

function selectPreset(key: string): void {
  void run(() => setProjectIcon(props.project.id, `preset:${key}`));
}

function removeIcon(): void {
  void run(() => clearProjectIcon(props.project.id));
}

function chooseFile(): void {
  fileInput.value?.click();
}

function onFile(event: Event): void {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) {
    return;
  }
  void run(() => uploadProjectIcon(props.project.id, file));
}
</script>

<template>
  <section class="project-icon-picker">
    <div class="project-icon-picker__head">
      <ProjectIcon :project="props.project" size="lg" />
      <NText strong>{{ t("projects.icon.title") }}</NText>
    </div>

    <div
      class="project-icon-picker__grid"
      role="group"
      :aria-label="t('projects.icon.presetsLabel')"
    >
      <NButton
        v-for="preset in projectIconPresets"
        :key="preset.key"
        quaternary
        circle
        class="project-icon-picker__preset"
        :class="{ 'is-active': currentPreset === preset.key }"
        :aria-pressed="currentPreset === preset.key"
        :aria-label="t(`projects.icon.presets.${preset.key}`)"
        :disabled="props.disabled || busy"
        @click="selectPreset(preset.key)"
      >
        <template #icon>
          <component :is="preset.component" />
        </template>
      </NButton>
    </div>

    <NSpace :size="8" align="center">
      <NButton
        secondary
        :disabled="props.disabled || busy"
        @click="chooseFile"
      >
        {{ busy ? t("projects.icon.uploading") : t("projects.icon.upload") }}
      </NButton>
      <NButton
        tertiary
        :disabled="props.disabled || busy || props.project.icon === ''"
        @click="removeIcon"
      >
        {{ t("projects.icon.remove") }}
      </NButton>
    </NSpace>

    <NText depth="3" class="project-icon-picker__hint">
      {{ t("projects.icon.hint") }}
    </NText>
    <NText v-if="errorMessage" type="error" role="alert">
      {{ errorMessage }}
    </NText>

    <input
      ref="fileInput"
      class="project-icon-picker__file"
      type="file"
      accept="image/png,image/jpeg,image/gif"
      hidden
      @change="onFile"
    />
  </section>
</template>

<style scoped>
.project-icon-picker {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  align-items: flex-start;
}

.project-icon-picker__head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.project-icon-picker__grid {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1);
}

.project-icon-picker__preset.is-active {
  background: var(--accent);
  color: var(--accent-on);
}

.project-icon-picker__hint {
  font-size: var(--text-xs);
}

.project-icon-picker__file {
  display: none;
}
</style>
