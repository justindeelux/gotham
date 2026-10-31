<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NCheckboxGroup,
  NForm,
  NFormItem,
  NInputNumber,
  NSwitch,
} from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import { getThreshold, updateThreshold } from "@/features/cleanup/api/center";
import type { CleanupCategory } from "@/features/cleanup/api/cleanup";
import {
  thresholdFormSchema,
  type ThresholdForm,
} from "@/features/cleanup/schemas/center";
import { failureText } from "@/features/servers/api/servers";

interface Props {
  categories: CleanupCategory[];
}

const props = defineProps<Props>();
const { t } = useI18n();

const form = ref<ThresholdForm>({
  enabled: false,
  percent: 85,
  categories: [],
  min_age_hours: 24,
  cooldown_hours: 6,
});
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const notice = ref("");

const availableCategories = computed(() =>
  props.categories.filter((category) => category.tier !== "dangerous"),
);

function categoryLabel(key: string): string {
  return t(`cleanup.categories.${key}`);
}

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    form.value = await getThreshold();
  } catch (cause) {
    error.value = failureText(cause);
  } finally {
    loading.value = false;
  }
}

async function save(): Promise<void> {
  const parsed = thresholdFormSchema.safeParse(form.value);
  if (!parsed.success) {
    error.value = t("cleanup.threshold.invalid");
    notice.value = "";
    return;
  }
  saving.value = true;
  error.value = "";
  notice.value = "";
  try {
    form.value = await updateThreshold(parsed.data);
    notice.value = t("cleanup.threshold.saved");
  } catch (cause) {
    error.value = failureText(cause);
  } finally {
    saving.value = false;
  }
}

onMounted(() => {
  void load();
});
</script>

<template>
  <NCard :title="t('cleanup.threshold.title')" class="cleanup-threshold">
    <p class="cleanup-threshold__desc">{{ t("cleanup.threshold.description") }}</p>

    <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>
    <NAlert v-if="notice" type="success" :show-icon="true">{{ notice }}</NAlert>

    <NForm label-placement="top" class="cleanup-threshold__form">
      <NFormItem :label="t('cleanup.threshold.enabled')">
        <NSwitch
          v-model:value="form.enabled"
          :disabled="loading || saving"
          data-testid="cleanup-threshold-enabled"
        />
      </NFormItem>

      <NFormItem :label="t('cleanup.threshold.percent')">
        <NInputNumber
          v-model:value="form.percent"
          :min="50"
          :max="99"
          :disabled="loading || saving"
          class="cleanup-threshold__number"
          data-testid="cleanup-threshold-percent"
        >
          <template #suffix>%</template>
        </NInputNumber>
      </NFormItem>

      <NFormItem :label="t('cleanup.threshold.categories')">
        <NCheckboxGroup v-model:value="form.categories" class="cleanup-threshold__categories">
          <NCheckbox
            v-for="category in availableCategories"
            :key="category.key"
            :value="category.key"
            :label="`${categoryLabel(category.key)} · ${t(`cleanup.tiers.${category.tier}`)}`"
          />
        </NCheckboxGroup>
      </NFormItem>

      <div class="cleanup-threshold__grid">
        <NFormItem :label="t('cleanup.threshold.minAge')">
          <NInputNumber
            v-model:value="form.min_age_hours"
            :min="1"
            :max="8760"
            :disabled="loading || saving"
            class="cleanup-threshold__number"
            data-testid="cleanup-threshold-min-age"
          >
            <template #suffix>{{ t("cleanup.servers.hours") }}</template>
          </NInputNumber>
        </NFormItem>

        <NFormItem :label="t('cleanup.threshold.cooldown')">
          <NInputNumber
            v-model:value="form.cooldown_hours"
            :min="1"
            :max="168"
            :disabled="loading || saving"
            class="cleanup-threshold__number"
            data-testid="cleanup-threshold-cooldown"
          >
            <template #suffix>{{ t("cleanup.servers.hours") }}</template>
          </NInputNumber>
        </NFormItem>
      </div>

      <NButton
        type="primary"
        :loading="saving"
        :disabled="loading"
        data-testid="cleanup-threshold-save"
        @click="() => void save()"
      >
        {{ t("cleanup.threshold.save") }}
      </NButton>
    </NForm>
  </NCard>
</template>

<style scoped>
.cleanup-threshold {
  min-width: 0;
}

.cleanup-threshold__desc {
  margin: 0 0 var(--space-3);
  max-width: 720px;
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-sm);
  color: var(--muted);
}

.cleanup-threshold__form {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.cleanup-threshold__categories {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-4);
  min-width: 0;
}

.cleanup-threshold__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: var(--space-3);
  min-width: 0;
}

.cleanup-threshold__number {
  width: 180px;
  max-width: 100%;
}

@media (max-width: 480px) {
  .cleanup-threshold__number {
    width: 100%;
  }
}
</style>
