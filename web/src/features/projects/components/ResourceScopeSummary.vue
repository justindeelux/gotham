<script setup lang="ts">
import { NAlert, NButton, NSelect, NText } from "naive-ui";
import { computed, ref, watch } from "vue";

import { useEnvironmentOptions } from "@/features/projects/composables/useEnvironmentOptions";
import { activeLocale, i18n } from "@/shared/i18n";

const props = withDefaults(
  defineProps<{
    projectId: string;
    environmentId: string;
    /**
     * Leading verb override (a translated sentence fragment). When absent
     * the creation wording renders in the active locale.
     */
    verb?: string;
  }>(),
  { verb: undefined },
);

const emit = defineEmits<{
  "update:projectId": [value: string];
  "update:environmentId": [value: string];
}>();

/**
 * Read-only project/environment summary with a change link, shared by every
 * create flow and the resource move card (PE-5, Linear JUS-34). The first
 * step of each creation shows where the resource lands; Change swaps the
 * summary for project and environment selects.
 */
const options = useEnvironmentOptions();

// Without a preselected scope (the flat template library) the selects show
// immediately instead of an empty summary.
const changing = ref(props.projectId === "" || props.environmentId === "");

/** t renders scope copy in the active locale (tracks language switches). */
function t(key: string): string {
  void activeLocale.value;
  return String(i18n.global.t(key));
}

/** verbText is the caller override or the localized creation wording. */
const verbText = computed<string>(() => props.verb ?? t("projects.scope.creatingIn"));

watch(
  () => props.projectId,
  (projectId) => {
    void options.fetchEnvironments(projectId);
  },
  { immediate: true },
);

const projectLabel = computed<string>(
  () =>
    options.projectNameOf(props.projectId) ||
    (options.projectsLoading.value ? t("projects.scope.loading") : props.projectId.slice(0, 8)),
);

const environmentLabel = computed<string>(
  () =>
    options.environmentNameOf(props.projectId, props.environmentId) ||
    (options.environmentsLoading.value ? t("projects.scope.loading") : props.environmentId.slice(0, 8)),
);

/** changeReady gates Done: both selects must name a scope. */
const changeReady = computed<boolean>(
  () => props.projectId !== "" && props.environmentId !== "",
);

/** handleProjectChange swaps the project and clears the stale environment. */
function handleProjectChange(projectId: string): void {
  emit("update:projectId", projectId);
  emit("update:environmentId", "");
  void options.fetchEnvironments(projectId);
}
</script>

<template>
  <div class="scope-summary">
    <div v-if="!changing" class="scope-summary__read">
      <NText depth="3">
        {{ verbText }}
        <span class="mono">{{ projectLabel }} / {{ environmentLabel }}</span>.
      </NText>
      <NButton text size="small" @click="changing = true">{{ t("projects.scope.change") }}</NButton>
    </div>

    <div v-else class="scope-summary__edit">
      <NAlert v-if="options.projectsError.value" type="error" :show-icon="true">
        {{ options.projectsError.value }}
      </NAlert>
      <div class="scope-summary__grid">
        <NSelect
          :value="props.projectId"
          :options="options.projectOptions.value"
          :loading="options.projectsLoading.value"
          :placeholder="t('projects.scope.projectPlaceholder')"
          :aria-label="t('projects.scope.projectAria')"
          @update:value="handleProjectChange"
        />
        <NSelect
          :value="props.environmentId"
          :options="options.environmentOptions(props.projectId)"
          :loading="options.environmentsLoading.value"
          :disabled="props.projectId === ''"
          :placeholder="t('projects.scope.environmentPlaceholder')"
          :aria-label="t('projects.scope.environmentAria')"
          @update:value="(value: string) => emit('update:environmentId', value)"
        />
      </div>
      <NAlert v-if="options.environmentsError.value" type="error" :show-icon="true">
        {{ options.environmentsError.value }}
      </NAlert>
      <div class="scope-summary__done">
        <NButton size="small" :disabled="!changeReady" @click="changing = false">
          {{ t("projects.scope.done") }}
        </NButton>
      </div>
    </div>
  </div>
</template>

<style scoped>
.scope-summary__read {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.scope-summary__edit {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.scope-summary__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 var(--space-3);
}

.scope-summary__done {
  display: flex;
  justify-content: flex-end;
}

.mono {
  font-family: var(--font-mono);
}

@container (max-width: 560px) {
  .scope-summary__grid {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
  }
}
</style>
