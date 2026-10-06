<script setup lang="ts">
import { NAlert, NButton, NCard, NInput, NSpace, NText } from "naive-ui";
import { toRef } from "vue";
import { useI18n } from "vue-i18n";

import type { Application } from "@/features/applications/api/applications";
import { useApplicationDomain } from "@/features/applications/composables/useApplicationDomain";

interface Props {
  application: Application;
}

const props = defineProps<Props>();

const domain = useApplicationDomain(toRef(props, "application"));

const { t } = useI18n();
</script>

<template>
  <NCard :title="t('applications.domain.title')">
    <NSpace vertical :size="12">
      <NText depth="3">
        {{ t("applications.domain.hint") }}
      </NText>
      <NAlert
        v-if="domain.domainError.value"
        type="error"
        :show-icon="true"
      >
        {{ domain.domainError.value }}
      </NAlert>
      <NSpace align="center" :size="8" :wrap="false">
        <NInput
          v-model:value="domain.baseDomain.value"
          class="mono"
          style="max-width: 360px"
          placeholder="app.example.com"
          :aria-label="t('applications.domain.baseAria')"
          @keyup.enter="domain.handleSaveDomain"
        />
        <NButton
          type="primary"
          :loading="domain.savingDomain.value"
          @click="domain.handleSaveDomain"
        >
          {{ t("applications.domain.save") }}
        </NButton>
      </NSpace>
      <NText depth="3" class="small">
        {{ t("applications.domain.emptyHint") }}
      </NText>
    </NSpace>
  </NCard>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}
</style>
