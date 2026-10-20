<script setup lang="ts">
import { NButton, NSelect } from "naive-ui";
import { useI18n } from "vue-i18n";

/**
 * Target selector + explicit refresh for the application Terminal tab
 * (JUS-247). The same control is used before the first shell and as the
 * service switch while one is live; only one instance is rendered at a time so
 * the shared label/input id stays unique.
 */
interface Props {
  options: Array<{ label: string; value: string }>;
  value: string;
  loading: boolean;
  disabled: boolean;
  label?: string;
}

withDefaults(defineProps<Props>(), { label: "" });
const emit = defineEmits<{
  select: [value: string];
  refresh: [];
}>();
const { t } = useI18n();
</script>

<template>
  <div class="app-term-picker">
    <label v-if="label !== ''" class="app-term-label" for="app-term-target">
      {{ label }}
    </label>
    <div class="app-term-picker-row">
      <NSelect
        id="app-term-target"
        class="app-term-select"
        :value="value"
        :options="options"
        :placeholder="t('applications.terminal.chooseService')"
        :loading="loading"
        :disabled="disabled"
        @update:value="emit('select', $event)"
      />
      <NButton
        secondary
        class="app-term-refresh"
        :loading="loading"
        @click="emit('refresh')"
      >
        {{ t("applications.terminal.refresh") }}
      </NButton>
    </div>
  </div>
</template>

<style scoped>
.app-term-picker {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.app-term-picker-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.app-term-label {
  font-size: var(--text-xs);
  color: var(--muted);
}

.app-term-select {
  min-width: 0;
  flex: 1 1 18rem;
}

.app-term-refresh {
  flex: 0 0 auto;
}

@media (max-width: 640px) {
  .app-term-picker-row {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
