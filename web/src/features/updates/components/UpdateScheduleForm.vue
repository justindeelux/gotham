<script setup lang="ts">
import type { FormInst } from "naive-ui";
import {
  NAlert,
  NButton,
  NCard,
  NForm,
  NFormItem,
  NInputNumber,
  NRadioButton,
  NRadioGroup,
  NSelect,
  NSwitch,
  useMessage,
} from "naive-ui";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { ScheduleState, UpdateFrequency } from "@/features/updates/schemas/updates";
import { scheduleRules } from "@/features/updates/schemas/updates";
import type { UpdateSchedule } from "@/features/updates/schemas/updates";

const props = defineProps<{
  state: ScheduleState;
  canEdit: boolean;
  saving: boolean;
  error: string;
  save: (_schedule: UpdateSchedule) => Promise<boolean>;
}>();

const { t } = useI18n();
const message = useMessage();
const formRef = ref<FormInst | null>(null);

interface ScheduleForm {
  checkEnabled: boolean;
  autoApply: boolean;
  channel: "stable" | "beta";
  frequency: UpdateFrequency;
  intervalHours: number | null;
  atTime: string;
  weekday: number;
}

function fromState(state: ScheduleState): ScheduleForm {
  const sc = state.schedule;
  return {
    checkEnabled: sc.check_enabled,
    autoApply: sc.auto_apply,
    channel: sc.channel,
    frequency: sc.frequency,
    intervalHours: Math.max(1, Math.round(sc.interval_minutes / 60)),
    atTime: sc.at_time,
    weekday: sc.weekday,
  };
}

const form = reactive<ScheduleForm>(fromState(props.state));
const rules = scheduleRules();

// A saved or reloaded schedule replaces the draft.
watch(() => props.state.schedule, () => Object.assign(form, fromState(props.state)));

const channelOptions = computed(() => [
  { label: t("updates.stats.channel.stable"), value: "stable" },
  { label: t("updates.stats.channel.beta"), value: "beta" },
]);
const weekdayOptions = computed(() =>
  [0, 1, 2, 3, 4, 5, 6].map((day) => ({
    label: t(`updates.schedule.weekdays.${day}`),
    value: day,
  })),
);

function onCheckChange(value: boolean): void {
  if (!value) {
    form.autoApply = false;
  }
}

async function onSubmit(): Promise<void> {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  const ok = await props.save({
    check_enabled: form.checkEnabled,
    auto_apply: form.checkEnabled && form.autoApply,
    channel: form.channel,
    frequency: form.frequency,
    interval_minutes: (form.intervalHours ?? 1) * 60,
    at_time: form.atTime,
    weekday: form.weekday,
  });
  if (ok) {
    message.success(t("updates.schedule.saved"));
  }
}
</script>

<template>
  <NCard :title="t('updates.schedule.title')" data-testid="update-schedule">
    <template #header-extra>
      <span class="meta">{{ t("updates.schedule.hint") }}</span>
    </template>
    <NAlert v-if="!canEdit" type="info" :show-icon="false" class="schedule-note">
      {{ t("updates.schedule.adminOnly") }}
    </NAlert>
    <NAlert v-if="error" type="error" class="schedule-note" data-testid="schedule-error">
      {{ t("updates.errors.save", { message: error }) }}
    </NAlert>
    <NForm ref="formRef" :model="form" :rules="rules" class="schedule-form" @submit.prevent="onSubmit">
      <NFormItem :show-label="false">
        <div class="switch-row">
          <NSwitch
            v-model:value="form.checkEnabled"
            :disabled="!canEdit"
            :aria-label="t('updates.schedule.checkEnabled')"
            @update:value="onCheckChange"
          />
          <div>
            <strong>{{ t("updates.schedule.checkEnabled") }}</strong>
            <p class="field-hint">{{ t("updates.schedule.checkHint") }}</p>
          </div>
        </div>
      </NFormItem>
      <NFormItem :show-label="false">
        <div class="switch-row">
          <NSwitch
            v-model:value="form.autoApply"
            :disabled="!canEdit || !form.checkEnabled"
            :aria-label="t('updates.schedule.autoApply')"
          />
          <div>
            <strong>{{ t("updates.schedule.autoApply") }}</strong>
            <p class="field-hint">{{ t("updates.schedule.autoApplyHint") }}</p>
          </div>
        </div>
      </NFormItem>
      <NFormItem :label="t('updates.schedule.channel')" path="channel">
        <NSelect v-model:value="form.channel" :options="channelOptions" :disabled="!canEdit" />
      </NFormItem>
      <NFormItem :label="t('updates.schedule.frequency')" path="frequency">
        <NRadioGroup v-model:value="form.frequency" :disabled="!canEdit || !form.checkEnabled">
          <NRadioButton value="interval">{{ t("updates.schedule.frequencies.interval") }}</NRadioButton>
          <NRadioButton value="daily">{{ t("updates.schedule.frequencies.daily") }}</NRadioButton>
          <NRadioButton value="weekly">{{ t("updates.schedule.frequencies.weekly") }}</NRadioButton>
        </NRadioGroup>
      </NFormItem>
      <NFormItem
        v-if="form.frequency === 'interval'"
        :label="t('updates.schedule.intervalHours')"
        path="intervalHours"
      >
        <NInputNumber
          v-model:value="form.intervalHours"
          :min="1"
          :max="720"
          :precision="0"
          :disabled="!canEdit || !form.checkEnabled"
        />
      </NFormItem>
      <template v-else>
        <NFormItem v-if="form.frequency === 'weekly'" :label="t('updates.schedule.weekday')" path="weekday">
          <NSelect
            v-model:value="form.weekday"
            :options="weekdayOptions"
            :disabled="!canEdit || !form.checkEnabled"
          />
        </NFormItem>
        <NFormItem :label="t('updates.schedule.time')" path="atTime">
          <input
            v-model="form.atTime"
            class="time-input"
            type="time"
            :disabled="!canEdit || !form.checkEnabled"
            :aria-label="t('updates.schedule.time')"
          />
        </NFormItem>
      </template>
      <p class="field-hint">{{ t("updates.schedule.timezone", { timezone: state.timezone }) }}</p>
      <div v-if="canEdit" class="schedule-actions">
        <NButton type="primary" attr-type="submit" :loading="saving">
          {{ t("updates.actions.save") }}
        </NButton>
        <NButton quaternary :disabled="saving" @click="Object.assign(form, fromState(state))">
          {{ t("updates.actions.reset") }}
        </NButton>
      </div>
    </NForm>
  </NCard>
</template>

<style scoped>
.schedule-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 320px), 1fr));
  grid-auto-flow: row dense;
  column-gap: var(--space-8);
  align-items: start;
  max-width: 960px;
}

/* Grid rows own the vertical rhythm; drop the global stacked-item margin. */
.n-form.schedule-form > * {
  margin-top: 0;
}

/* Switch rows, frequency, hints and actions span both columns. */
.schedule-form > .n-form-item:has(.switch-row),
.schedule-form > .n-form-item:has(.n-radio-group),
.schedule-form > .field-hint,
.schedule-form > .schedule-actions {
  grid-column: 1 / -1;
}

.schedule-note {
  margin-bottom: var(--space-3);
}

.schedule-actions {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-3);
}

.field-hint {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--meta);
}

.time-input {
  font-family: var(--font-mono);
  color: var(--fg);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 4px 8px;
  color-scheme: dark;
}
</style>
