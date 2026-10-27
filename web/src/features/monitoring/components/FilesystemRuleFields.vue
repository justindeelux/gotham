<script setup lang="ts">
import { NFormItem, NInputNumber, NSwitch } from "naive-ui";
import { useI18n } from "vue-i18n";

import type { FilesystemRule } from "@/features/monitoring/api/monitoring";

const rule = defineModel<FilesystemRule>("rule", { required: true });
const { t } = useI18n();
</script>

<template>
  <div class="rule-grid">
    <NFormItem :label="t('monitoring.rules.enabled')">
      <NSwitch
        v-model:value="rule.enabled"
        :aria-label="t('monitoring.rules.enabled')"
      />
    </NFormItem>
    <NFormItem :label="t('monitoring.rules.warning')">
      <NInputNumber
        v-model:value="rule.warning"
        :min="0.1"
        :max="100"
        :step="1"
        :input-props="{ 'aria-label': t('monitoring.rules.warning') }"
      >
        <template #suffix>%</template>
      </NInputNumber>
    </NFormItem>
    <NFormItem :label="t('monitoring.rules.critical')">
      <NInputNumber
        v-model:value="rule.critical"
        :min="0.1"
        :max="100"
        :step="1"
        :input-props="{ 'aria-label': t('monitoring.rules.critical') }"
      >
        <template #suffix>%</template>
      </NInputNumber>
    </NFormItem>
    <NFormItem :label="t('monitoring.rules.downgrade')">
      <NInputNumber
        v-model:value="rule.downgrade"
        :min="0.1"
        :max="100"
        :step="1"
        :input-props="{ 'aria-label': t('monitoring.rules.downgrade') }"
      >
        <template #suffix>%</template>
      </NInputNumber>
    </NFormItem>
    <NFormItem :label="t('monitoring.rules.clear')">
      <NInputNumber
        v-model:value="rule.clear"
        :min="0.1"
        :max="100"
        :step="1"
        :input-props="{ 'aria-label': t('monitoring.rules.clear') }"
      >
        <template #suffix>%</template>
      </NInputNumber>
    </NFormItem>
    <NFormItem :label="t('monitoring.rules.confirm')">
      <NInputNumber
        v-model:value="rule.confirm_seconds"
        :min="10"
        :max="3600"
        :step="10"
        :input-props="{ 'aria-label': t('monitoring.rules.confirm') }"
      >
        <template #suffix>s</template>
      </NInputNumber>
    </NFormItem>
    <NFormItem :label="t('monitoring.rules.criticalFor')">
      <NInputNumber
        v-model:value="rule.critical_seconds"
        :min="10"
        :max="3600"
        :step="10"
        :input-props="{ 'aria-label': t('monitoring.rules.criticalFor') }"
      >
        <template #suffix>s</template>
      </NInputNumber>
    </NFormItem>
    <NFormItem :label="t('monitoring.rules.downgradeFor')">
      <NInputNumber
        v-model:value="rule.downgrade_seconds"
        :min="10"
        :max="3600"
        :step="10"
        :input-props="{ 'aria-label': t('monitoring.rules.downgradeFor') }"
      >
        <template #suffix>s</template>
      </NInputNumber>
    </NFormItem>
    <NFormItem :label="t('monitoring.rules.clearFor')">
      <NInputNumber
        v-model:value="rule.clear_seconds"
        :min="10"
        :max="3600"
        :step="10"
        :input-props="{ 'aria-label': t('monitoring.rules.clearFor') }"
      >
        <template #suffix>s</template>
      </NInputNumber>
    </NFormItem>
  </div>
</template>

<style scoped>
.rule-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: var(--space-3);
}
</style>
