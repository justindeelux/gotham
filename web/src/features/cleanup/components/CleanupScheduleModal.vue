<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCheckbox,
  NCheckboxGroup,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NRadio,
  NRadioGroup,
  NSwitch,
} from "naive-ui";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { nextCronRun } from "@/features/cleanup/api/center";
import {
  scheduleFormSchema,
  type CleanupSchedule,
  type ScheduleForm,
} from "@/features/cleanup/schemas/center";
import { cronPresets, cronPresetKey } from "@/features/cleanup/utils/cron";
import type { CleanupCategory } from "@/features/cleanup/api/cleanup";

interface ServerOption {
  id: string;
  name: string;
}

interface Props {
  show: boolean;
  schedule: CleanupSchedule | null;
  servers: ServerOption[];
  categories: CleanupCategory[];
  saving: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
  save: [form: ScheduleForm];
}>();

const { t } = useI18n();

const name = ref("");
const serverIDs = ref<string[]>([]);
const selectedCategories = ref<string[]>([]);
const preset = ref<"daily" | "weekly" | "custom">("daily");
const cron = ref("0 3 * * *");
const minAgeHours = ref(24);
const enabled = ref(true);
const formError = ref("");
const nextRunPreview = ref("");
const cronError = ref("");
let previewTimer: ReturnType<typeof setTimeout> | undefined;
let previewRequest = 0;

const availableCategories = computed(() =>
  props.categories.filter((category) => category.tier !== "dangerous"),
);

function reset(): void {
  const schedule = props.schedule;
  name.value = schedule?.name ?? "";
  serverIDs.value = schedule ? [...schedule.server_ids] : [];
  selectedCategories.value = schedule
    ? [...schedule.categories]
    : availableCategories.value
        .filter((category) => category.tier === "safe")
        .map((category) => category.key);
  preset.value = schedule ? cronPresetKey(schedule.cron) : "daily";
  cron.value = schedule?.cron ?? cronPresets[0]?.cron ?? "0 3 * * *";
  minAgeHours.value = schedule?.min_age_hours ?? 24;
  enabled.value = schedule?.enabled ?? true;
  formError.value = "";
  nextRunPreview.value = "";
  cronError.value = "";
  schedulePreview();
}

// The server resolves the slot with the same parser the scheduler uses, so the
// preview cannot drift from the real run time.
function schedulePreview(): void {
  if (previewTimer) {
    clearTimeout(previewTimer);
  }
  const expression = cron.value.trim();
  if (expression === "") {
    nextRunPreview.value = "";
    cronError.value = "";
    return;
  }
  previewTimer = setTimeout(() => {
    const request = ++previewRequest;
    nextCronRun(expression)
      .then((next) => {
        if (request !== previewRequest) {
          return;
        }
        nextRunPreview.value = next;
        cronError.value = "";
      })
      .catch(() => {
        if (request !== previewRequest) {
          return;
        }
        nextRunPreview.value = "";
        cronError.value = t("cleanup.schedules.form.cronInvalid");
      });
  }, 250);
}

watch(cron, schedulePreview);

onBeforeUnmount(() => {
  if (previewTimer) {
    clearTimeout(previewTimer);
  }
});

watch(
  () => props.show,
  (show) => {
    if (show) {
      reset();
    }
  },
);

function choosePreset(value: "daily" | "weekly" | "custom"): void {
  preset.value = value;
  if (value === "custom") {
    return;
  }
  const chosen = cronPresets.find((option) => option.key === value);
  if (chosen) {
    cron.value = chosen.cron;
  }
}

function categoryLabel(key: string): string {
  return t(`cleanup.categories.${key}`);
}

function submit(): void {
  const result = scheduleFormSchema.safeParse({
    name: name.value,
    server_ids: serverIDs.value,
    categories: selectedCategories.value,
    cron: cron.value,
    min_age_hours: minAgeHours.value,
    enabled: enabled.value,
  });
  if (!result.success) {
    formError.value = t("cleanup.schedules.form.invalid");
    return;
  }
  formError.value = "";
  emit("save", result.data);
}
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    :title="schedule ? t('cleanup.schedules.editTitle') : t('cleanup.schedules.createTitle')"
    :mask-closable="false"
    class="app-modal cleanup-schedule-modal"
    style="width: 640px; max-width: 96vw"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <NForm label-placement="top" class="cleanup-schedule-form">
      <NAlert v-if="formError" type="error" :show-icon="true">
        {{ formError }}
      </NAlert>

      <NFormItem :label="t('cleanup.schedules.form.name')" required>
        <NInput
          v-model:value="name"
          :maxlength="80"
          :placeholder="t('cleanup.schedules.form.namePlaceholder')"
        />
      </NFormItem>

      <NFormItem :label="t('cleanup.schedules.form.servers')" required>
        <NCheckboxGroup v-model:value="serverIDs" class="cleanup-schedule-servers">
          <NCheckbox
            v-for="server in servers"
            :key="server.id"
            :value="server.id"
            :label="server.name"
          />
        </NCheckboxGroup>
      </NFormItem>

      <NFormItem :label="t('cleanup.schedules.form.categories')" required>
        <NCheckboxGroup v-model:value="selectedCategories" class="cleanup-schedule-categories">
          <NCheckbox
            v-for="category in availableCategories"
            :key="category.key"
            :value="category.key"
            :label="`${categoryLabel(category.key)} · ${t(`cleanup.tiers.${category.tier}`)}`"
          />
        </NCheckboxGroup>
      </NFormItem>

      <NFormItem :label="t('cleanup.schedules.form.cron')" required class="cleanup-schedule-cron-item">
        <NRadioGroup
          :value="preset"
          class="cleanup-schedule-presets"
          @update:value="(value: 'daily' | 'weekly' | 'custom') => choosePreset(value)"
        >
          <NRadio value="daily">{{ t("cleanup.schedules.form.daily") }}</NRadio>
          <NRadio value="weekly">{{ t("cleanup.schedules.form.weekly") }}</NRadio>
          <NRadio value="custom">{{ t("cleanup.schedules.form.custom") }}</NRadio>
        </NRadioGroup>
        <NInput
          v-model:value="cron"
          class="cleanup-schedule-cron"
          :placeholder="t('cleanup.schedules.form.cronPlaceholder')"
          @update:value="preset = cronPresetKey(cron)"
        />
        <p v-if="cronError" class="cleanup-schedule-hint cleanup-schedule-hint--error">
          {{ cronError }}
        </p>
        <p v-else-if="nextRunPreview" class="cleanup-schedule-hint">
          {{ t("cleanup.schedules.form.cronNext", {
            time: new Date(nextRunPreview).toLocaleString(),
          }) }}
        </p>
      </NFormItem>

      <NFormItem :label="t('cleanup.schedules.form.minAge')">
        <NInputNumber
          v-model:value="minAgeHours"
          :min="1"
          :max="8760"
          class="cleanup-schedule-age"
        >
          <template #suffix>{{ t("cleanup.servers.hours") }}</template>
        </NInputNumber>
      </NFormItem>

      <NFormItem :label="t('cleanup.schedules.form.enabled')">
        <NSwitch v-model:value="enabled" />
      </NFormItem>
    </NForm>

    <template #footer>
      <div class="cleanup-schedule-footer">
        <NButton @click="emit('update:show', false)">
          {{ t("cleanup.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="saving"
          data-testid="cleanup-schedule-save"
          @click="submit"
        >
          {{ t("cleanup.schedules.form.save") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.cleanup-schedule-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.cleanup-schedule-servers,
.cleanup-schedule-categories {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-4);
}

.cleanup-schedule-presets {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}

.cleanup-schedule-cron-item :deep(.n-form-item-blank) {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: var(--space-2);
  width: 100%;
}

.cleanup-schedule-cron {
  font-family: var(--font-mono);
}

.cleanup-schedule-hint {
  margin: 0;
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--muted);
}

.cleanup-schedule-hint--error {
  color: var(--danger);
}

.cleanup-schedule-age {
  width: 160px;
}

.cleanup-schedule-footer {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: var(--space-2);
}

@media (max-width: 480px) {
  .cleanup-schedule-age {
    width: 100%;
  }
}
</style>
